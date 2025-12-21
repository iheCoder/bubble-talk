package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/domain"
	"bubble-talk/server/internal/gateway"
	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/orchestrator"
	"bubble-talk/server/internal/realtime"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	config       *config.Config
	store        session.Store
	timeline     timeline.Store
	bubbles      []model.Bubble
	now          func() time.Time
	orchestrator *orchestrator.Orchestrator

	// gateways 管理所有活跃的语音网关 (sessionID -> Gateway/MultiVoiceGateway)
	gateways   map[string]interface{}
	gatewaysMu sync.RWMutex

	// realtimeClient 只用于签发 OpenAI Realtime 的 ephemeral key，
	// 让浏览器用 WebRTC 直连 OpenAI（语音原生），同时不暴露服务端 API Key。
	realtimeClient *realtime.Client

	// WebSocket upgrader
	upgrader websocket.Upgrader
}

func NewServer(cfg *config.Config, store session.Store, timeline timeline.Store) (*Server, error) {
	bubbles, err := domain.LoadBubbles(cfg.Paths.Bubbles)
	if err != nil {
		return nil, err
	}

	return &Server{
		config:       cfg,
		store:        store,
		timeline:     timeline,
		bubbles:      bubbles,
		now:          time.Now,
		orchestrator: orchestrator.New(store, timeline, time.Now),
		gateways:     make(map[string]interface{}),
		realtimeClient: &realtime.Client{
			APIKey: cfg.OpenAI.APIKey,
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// 开发期允许本地跨域，生产环境应改为白名单
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173"
			},
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
	engine.GET("/api/sessions/:id/stream", s.handleSessionStream)
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
		AvailableRoles:    bubble.Roles, // 从泡泡配置中获取角色列表
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

// handleSessionStream 处理 WebSocket 连接，创建 Gateway 并启动双向语音流
func (s *Server) handleSessionStream(c *gin.Context) {
	sessionID := c.Param("id")
	log.Printf("[API] 📞 WebSocket connection request for session: %s", sessionID)
	log.Printf("[API] Client address: %s", c.Request.RemoteAddr)
	log.Printf("[API] Origin: %s", c.Request.Header.Get("Origin"))

	// 验证 Session 存在
	state, err := s.store.Get(c.Request.Context(), sessionID)
	if err != nil {
		if err == session.ErrNotFound {
			log.Printf("[API] ❌ Session not found: %s", sessionID)
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		log.Printf("[API] ❌ Failed to load session %s: %v", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load session failed"})
		return
	}
	log.Printf("[API] ✅ Session validated: entry_id=%s domain=%s", state.EntryID, state.Domain)

	// 升级到 WebSocket
	log.Printf("[API] Upgrading to WebSocket...")
	clientConn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[API] ❌ Failed to upgrade websocket: %v", err)
		return
	}
	log.Printf("[API] ✅ WebSocket upgraded successfully")

	// 创建 Gateway 配置：只为当前泡泡配置的角色创建 RoleProfiles
	roleProfiles := make(map[string]gateway.RoleProfile)
	for _, role := range state.AvailableRoles {
		if profile, ok := s.config.Roles[role]; ok {
			roleProfiles[role] = gateway.RoleProfile{
				Voice:  profile.Voice,
				Avatar: profile.Avatar,
			}
		} else {
			log.Printf("[API] ⚠️  Role %s not found in global config, skipping", role)
		}
	}

	if len(roleProfiles) == 0 {
		log.Printf("[API] ⚠️  No valid roles found, using default roles")
		// 兜底：如果没有有效角色，使用全部配置的角色
		for role, profile := range s.config.Roles {
			roleProfiles[role] = gateway.RoleProfile{
				Voice:  profile.Voice,
				Avatar: profile.Avatar,
			}
		}
	}

	log.Printf("[API] Creating RoleProfiles for roles: %v", state.AvailableRoles)

	gwConfig := gateway.GatewayConfig{
		OpenAIAPIKey:                 s.config.OpenAI.APIKey,
		OpenAIRealtimeURL:            s.config.OpenAI.RealtimeURL,
		Model:                        s.config.OpenAI.Model,
		Voice:                        s.config.OpenAI.Voice,
		RoleProfiles:                 roleProfiles,
		DefaultInstructions:          s.config.Gateway.DefaultInstructions,
		ReadTimeout:                  30 * time.Second,
		WriteTimeout:                 30 * time.Second,
		PingInterval:                 s.config.Gateway.PingInterval,
		InputAudioFormat:             s.config.Gateway.InputAudioFormat,
		OutputAudioFormat:            s.config.Gateway.OutputAudioFormat,
		InputAudioTranscriptionModel: s.config.Gateway.InputAudioTranscriptionModel,
	}
	log.Printf("[API] Gateway config: model=%s voice=%s", gwConfig.Model, gwConfig.Voice)

	// 创建 MultiVoiceGateway 实例（支持多音色）
	log.Printf("[API] Creating MultiVoiceGateway instance with %d roles...", len(roleProfiles))
	gw := gateway.NewMultiVoiceGateway(sessionID, clientConn, gwConfig)

	// 设置事件处理器：将 Gateway 事件转发给 Orchestrator
	gw.SetEventHandler(func(ctx context.Context, msg *gateway.ClientMessage) error {
		return s.handleGatewayEvent(ctx, sessionID, gw, msg)
	})

	// 注册到活跃网关表
	s.gatewaysMu.Lock()
	s.gateways[sessionID] = gw
	gatewayCount := len(s.gateways)
	s.gatewaysMu.Unlock()
	log.Printf("[API] Gateway registered (total active: %d)", gatewayCount)

	// 清理函数
	defer func() {
		s.gatewaysMu.Lock()
		delete(s.gateways, sessionID)
		remaining := len(s.gateways)
		s.gatewaysMu.Unlock()
		_ = gw.Close()
		log.Printf("[API] 🔌 Gateway closed for session %s (remaining: %d)", sessionID, remaining)
	}()

	// 获取初始指令
	log.Printf("[API] Getting initial instructions from Orchestrator...")
	instructions, err := s.orchestrator.GetInitialInstructions(c.Request.Context(), state)
	if err != nil {
		log.Printf("[API] ⚠️  Failed to get initial instructions: %v, using fallback", err)
		instructions = gwConfig.DefaultInstructions
	} else {
		log.Printf("[API] ✅ Initial instructions generated (%d chars)", len(instructions))
	}

	// 更新 Gateway 配置中的指令
	gwConfig.DefaultInstructions = instructions

	// 启动 Gateway（连接 OpenAI Realtime）
	log.Printf("[API] Starting Gateway...")
	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Printf("[API] ❌ Failed to start gateway: %v", err)
		_ = clientConn.Close()
		return
	}

	log.Printf("[API] ✅ Gateway started successfully for session %s", sessionID)
	log.Printf("[API] 🎙️  Ready for audio streaming...")

	// 阻塞直到连接关闭
	<-gw.Done()
	log.Printf("[API] Gateway connection closed for session %s", sessionID)
}

// handleGatewayEvent 处理来自 Gateway 的事件
func (s *Server) handleGatewayEvent(ctx context.Context, sessionID string, gw interface{}, msg *gateway.ClientMessage) error {
	log.Printf("[API] gateway event: session=%s type=%s", sessionID, msg.Type)

	switch msg.Type {
	case gateway.EventTypeASRFinal:
		// 用户语音转写完成，交给 Orchestrator 处理
		return s.orchestrator.HandleUserUtterance(ctx, sessionID, msg.Text, gw)

	case gateway.EventTypeAssistantText:
		fromRole := ""
		if msg.Metadata != nil {
			if v, ok := msg.Metadata["role"].(string); ok {
				fromRole = v
			}
		}
		return s.orchestrator.HandleAssistantText(ctx, sessionID, msg.Text, fromRole)

	case gateway.EventTypeQuizAnswer:
		// 用户答题
		return s.orchestrator.HandleQuizAnswer(ctx, sessionID, msg.QuestionID, msg.Answer)

	case gateway.EventTypeBargeIn:
		// 用户插话中断，记录事件即可（Gateway 已处理取消逻辑）
		event := &model.Event{
			EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			SessionID: sessionID,
			Type:      "barge_in",
			ClientTS:  msg.ClientTS,
			ServerTS:  time.Now(),
		}
		_, err := s.timeline.Append(ctx, sessionID, event)
		return err

	case gateway.EventTypeExitRequested:
		// 用户请求退出
		event := &model.Event{
			EventID:   fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			SessionID: sessionID,
			Type:      "exit_requested",
			ClientTS:  msg.ClientTS,
			ServerTS:  time.Now(),
		}
		if _, err := s.timeline.Append(ctx, sessionID, event); err != nil {
			return err
		}
		// TODO: 触发 EXIT_TICKET 流程
		return nil
	case gateway.EventTypeWorldEntered:
		// World 进入，导演主动开场
		return s.orchestrator.HandleWorldEntered(ctx, sessionID, gw)

	default:
		log.Printf("[API] unhandled gateway event type: %s", msg.Type)
		return nil
	}
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

	modelName := s.config.OpenAI.Model
	if modelName == "" {
		modelName = os.Getenv("OPENAI_REALTIME_MODEL")
	}
	if modelName == "" {
		modelName = "gpt-realtime-2025-08-28"
	}
	voice := s.config.OpenAI.Voice
	if voice == "" {
		voice = os.Getenv("OPENAI_REALTIME_VOICE")
	}
	if voice == "" {
		voice = "alloy"
	}

	// 注意：这里的 instructions 只是第一阶段的"最小可用"，
	// 后续应改为：Orchestrator/Director 每轮动态更新（session.update）。
	// 使用 Orchestrator 获取初始指令，确保与 ActorEngine 逻辑一致
	instructions, err := s.orchestrator.GetInitialInstructions(c.Request.Context(), state)
	if err != nil {
		log.Printf("failed to get initial instructions: %v, using fallback", err)
		instructions = "你是 BubbleTalk 的语音教学助手。用中文、口语化、短句输出。"
	}

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
