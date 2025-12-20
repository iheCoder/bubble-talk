package api

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"bubble-talk/server/internal/domain"
	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/orchestrator"
	"bubble-talk/server/internal/realtime"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"

	"github.com/gin-gonic/gin"
)

type Server struct {
	store        session.Store
	timeline     timeline.Store
	bubbles      []model.Bubble
	now          func() time.Time
	orchestrator *orchestrator.Orchestrator

	// realtimeClient 只用于签发 OpenAI Realtime 的 ephemeral key，
	// 让浏览器用 WebRTC 直连 OpenAI（语音原生），同时不暴露服务端 API Key。
	realtimeClient *realtime.Client
}

func NewServer(store session.Store, timeline timeline.Store, bubblesPath string) (*Server, error) {
	bubbles, err := domain.LoadBubbles(bubblesPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		store:        store,
		timeline:     timeline,
		bubbles:      bubbles,
		now:          time.Now,
		orchestrator: orchestrator.New(store, timeline, time.Now),
		realtimeClient: &realtime.Client{
			APIKey: os.Getenv("OPENAI_API_KEY"),
		},
	}, nil
}

func (s *Server) Routes() http.Handler {
	// Gin 统一承载中间件与路由，便于扩展日志/鉴权/限流等能力。
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery(), s.corsMiddleware())
	engine.GET("/healthz", s.handleHealthz)
	engine.GET("/api/bubbles", s.handleBubbles)
	engine.POST("/api/sessions", s.handleSessions)
	engine.POST("/api/sessions/:id/events", s.handleSessionEvents)
	engine.POST("/api/sessions/:id/realtime/token", s.handleRealtimeToken)
	return engine
}

// handleHealthz 返回服务健康状态。
func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleBubbles 返回所有可用的泡泡。
func (s *Server) handleBubbles(c *gin.Context) {
	c.JSON(http.StatusOK, s.bubbles)
}

type createSessionRequest struct {
	EntryID string `json:"entry_id"`
}

// handleSessions 处理 /api/sessions 路由，支持创建新 Session。
func (s *Server) handleSessions(c *gin.Context) {
	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if req.EntryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "entry_id required"})
		return
	}

	bubble, ok := findBubble(s.bubbles, req.EntryID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "entry_id not found"})
		return
	}

	now := s.now()
	state := model.SessionState{
		SessionID:         newSessionID(),
		EntryID:           bubble.EntryID,
		Domain:            bubble.Domain,
		MainObjective:     bubble.Title,
		Act:               1,
		Beat:              "ColdOpen",
		PacingMode:        "NORMAL",
		MasteryEstimate:   0.2,
		OutputClockSec:    0,
		LastOutputAt:      now,
		TensionLevel:      2,
		CognitiveLoad:     2,
		QuestionStack:     nil,
		Signals:           model.SignalsSnapshot{},
		Turns:             nil,
		MisconceptionTags: nil,
	}

	// 副作用：创建快照以便后续 reducer 增量归约。
	if err := s.store.Save(c.Request.Context(), &state); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save session failed"})
		return
	}

	resp := model.CreateSessionResponse{
		SessionID: state.SessionID,
		State:     state,
		Diagnose:  defaultDiagnose(),
	}
	c.JSON(http.StatusOK, resp)
}

// handleSessionEvents 处理 /api/sessions/{id}/events 路由，接收用户事件。
func (s *Server) handleSessionEvents(c *gin.Context) {
	var evt model.Event
	if err := c.ShouldBindJSON(&evt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	sessionID := c.Param("id")
	// 这里将事件交给编排器，确保走 append-first 与快照归约。
	resp, err := s.orchestrator.OnEvent(c.Request.Context(), sessionID, evt)
	if err != nil {
		if err == session.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handle event failed"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

type realtimeTokenResponse struct {
	Model        string `json:"model"`
	Voice        string `json:"voice"`
	EphemeralKey string `json:"ephemeral_key"`
	ExpiresAt    int64  `json:"expires_at"`

	// instructions 是给 gpt-realtime 的系统指令，建议服务端按 session 动态生成，
	// 以确保“导演约束”稳定可控（对话第一公民）。
	Instructions string `json:"instructions"`
}

// handleRealtimeToken 处理 /api/sessions/{id}/realtime/token 路由，签发 Realtime ephemeral key。
func (s *Server) handleRealtimeToken(c *gin.Context) {
	id := c.Param("id")
	state, err := s.store.Get(c.Request.Context(), id)
	if err != nil {
		if err == session.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load session failed"})
		return
	}

	// 第一阶段先走环境变量，后续可把每个泡泡的 voice profile 配置化。
	modelName := os.Getenv("OPENAI_REALTIME_MODEL")
	if modelName == "" {
		modelName = "gpt-4o-realtime-preview"
	}
	voice := os.Getenv("OPENAI_REALTIME_VOICE")
	if voice == "" {
		voice = "alloy"
	}

	// 注意：这里的 instructions 只是第一阶段的“最小可用”，
	// 后续应改为：Orchestrator/Director 每轮动态更新（session.update）。
	instructions := fmt.Sprintf(
		"你是 BubbleTalk 的语音教学助手。默认用中文、口语化、短句输出。"+
			"本次泡泡主题：%s。当前主目标：%s。"+
			"对话规则：每 90 秒必须让用户完成一次输出动作（复述/选择/举例/迁移）。"+
			"如果用户说“我懂了/结束”，必须立刻给出迁移检验（Exit Ticket）。",
		state.EntryID,
		state.MainObjective,
	)

	keyResp, err := s.realtimeClient.CreateEphemeralKey(c.Request.Context(), realtime.CreateSessionRequest{
		Model:        modelName,
		Voice:        voice,
		Instructions: instructions,
	})
	if err != nil {
		// 这里记录详细错误到服务端日志，返回给前端的错误保持简洁，避免误泄漏信息。
		log.Printf("create realtime token failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create realtime token failed"})
		return
	}

	c.JSON(http.StatusOK, realtimeTokenResponse{
		Model:        modelName,
		Voice:        voice,
		EphemeralKey: keyResp.ClientSecret.Value,
		ExpiresAt:    keyResp.ClientSecret.ExpiresAt,
		Instructions: instructions,
	})
}

func findBubble(bubbles []model.Bubble, entryID string) (model.Bubble, bool) {
	for _, b := range bubbles {
		if b.EntryID == entryID {
			return b, true
		}
	}
	return model.Bubble{}, false
}

func defaultDiagnose() model.DiagnoseSet {
	return model.DiagnoseSet{
		Questions: []model.QuizQuestion{
			{
				ID:      "diag_q1",
				Prompt:  "机会成本更接近以下哪个含义？",
				Options: []string{"花出去的钱", "放弃的最好替代价值", "工资收入"},
			},
			{
				ID:      "diag_q2",
				Prompt:  "周末加班的机会成本最可能是？",
				Options: []string{"多赚的钱", "失去的休息或副业机会", "加班餐补"},
			},
		},
	}
}

func newSessionID() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("S_%d", now)
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		// 开发期：允许本地 Vite；线上应改为白名单或同源。
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
