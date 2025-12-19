package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bubble-talk/server/internal/domain"
	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/realtime"
	"bubble-talk/server/internal/session"
)

type Server struct {
	store   session.Store
	bubbles []model.Bubble
	now     func() time.Time

	// realtimeClient 只用于签发 OpenAI Realtime 的 ephemeral key，
	// 让浏览器用 WebRTC 直连 OpenAI（语音原生），同时不暴露服务端 API Key。
	realtimeClient *realtime.Client
}

func NewServer(store session.Store, bubblesPath string) (*Server, error) {
	bubbles, err := domain.LoadBubbles(bubblesPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		store:   store,
		bubbles: bubbles,
		now:     time.Now,
		realtimeClient: &realtime.Client{
			APIKey: os.Getenv("OPENAI_API_KEY"),
		},
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/bubbles", s.handleBubbles)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionSubroutes)

	// 开发期方便前端本地起 Vite（5173）直连后端。
	// 线上建议用反向代理统一域名，并收紧 CORS。
	return withCORS(mux)
}

// handleHealthz 返回服务健康状态。
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBubbles 返回所有可用的泡泡
func (s *Server) handleBubbles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, s.bubbles)
}

type createSessionRequest struct {
	EntryID string `json:"entry_id"`
}

// handleSessions 处理 /api/sessions 路由，支持创建新 Session。
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.EntryID == "" {
		writeError(w, http.StatusBadRequest, "entry_id required")
		return
	}

	bubble, ok := findBubble(s.bubbles, req.EntryID)
	if !ok {
		writeError(w, http.StatusNotFound, "entry_id not found")
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

	if err := s.store.Save(r.Context(), &state); err != nil {
		writeError(w, http.StatusInternalServerError, "save session failed")
		return
	}

	resp := model.CreateSessionResponse{
		SessionID: state.SessionID,
		State:     state,
		Diagnose:  defaultDiagnose(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSessionSubroutes 处理 /api/sessions/{id}/... 的子路由。
func (s *Server) handleSessionSubroutes(w http.ResponseWriter, r *http.Request) {
	// 支持两类子路由：
	// - /api/sessions/{id}/events          ：兼容纯文本/测试环境的事件入口
	// - /api/sessions/{id}/realtime/token  ：签发 OpenAI Realtime ephemeral key（WebRTC 直连）

	path, ok := strings.CutPrefix(r.URL.Path, "/api/sessions/")
	if !ok || path == "" {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// 期望 path 形如：{id}/events 或 {id}/realtime/token
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	id := parts[0]
	tail := strings.Join(parts[1:], "/")

	switch {
	case tail == "events":
		s.handleSessionEvents(w, r, id)
		return
	case tail == "realtime/token":
		s.handleRealtimeToken(w, r, id)
		return
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
}

// handleSessionEvents 处理 /api/sessions/{id}/events 路由，接收用户事件。
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	state, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load session failed")
		return
	}

	var evt model.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	now := s.now()
	if !state.LastOutputAt.IsZero() {
		state.OutputClockSec = int(now.Sub(state.LastOutputAt).Seconds())
	}
	state.Signals.LastUserChars = len(evt.Text)

	if evt.ClientTS.IsZero() {
		evt.ClientTS = now
	}
	state.Turns = append(state.Turns, model.Turn{
		Role: "user",
		Text: evt.Text,
		TS:   evt.ClientTS,
	})

	plan := model.DirectorPlan{
		UserMindState:     []string{"Partial"},
		Intent:            "Clarify",
		NextBeat:          "Check",
		NextRole:          "Coach",
		OutputAction:      "Recap",
		TalkBurstLimitSec: 20,
		TensionGoal:       "keep",
		LoadGoal:          "keep",
		StackAction:       "keep",
		Notes:             "stub plan for stage-1",
	}

	assistantText := "收到。先用一句话复述你的理解，我们再往下走。"
	state.Turns = append(state.Turns, model.Turn{
		Role: "assistant",
		Text: assistantText,
		TS:   now,
	})
	state.OutputClockSec = 0
	state.LastOutputAt = now

	if err := s.store.Save(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, "save session failed")
		return
	}

	resp := model.EventResponse{
		Assistant: model.AssistantMessage{
			Text: assistantText,
			NeedUserAction: &model.UserAction{
				Type:   "recap",
				Prompt: "用一句话复述，必须包含因为…所以…",
			},
			Quiz: nil,
		},
		Debug: &model.DebugPayload{DirectorPlan: plan},
	}

	writeJSON(w, http.StatusOK, resp)
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
func (s *Server) handleRealtimeToken(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	state, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, session.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load session failed")
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

	keyResp, err := s.realtimeClient.CreateEphemeralKey(r.Context(), realtime.CreateSessionRequest{
		Model:        modelName,
		Voice:        voice,
		Instructions: instructions,
	})
	if err != nil {
		// 这里记录详细错误到服务端日志，返回给前端的错误保持简洁，避免误泄漏信息。
		log.Printf("create realtime token failed: %v", err)
		writeError(w, http.StatusInternalServerError, "create realtime token failed")
		return
	}

	writeJSON(w, http.StatusOK, realtimeTokenResponse{
		Model:        modelName,
		Voice:        voice,
		EphemeralKey: keyResp.ClientSecret.Value,
		ExpiresAt:    keyResp.ClientSecret.ExpiresAt,
		Instructions: instructions,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// 开发期：允许本地 Vite；线上应改为白名单或同源。
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
