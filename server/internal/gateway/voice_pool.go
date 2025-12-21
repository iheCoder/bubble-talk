package gateway

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// VoicePool 管理多个角色连接（每个角色一个固定音色的 Realtime 连接）
// 核心职责：
// 1. 为每个角色维护独立的 RoleConn
// 2. 维护一个 ASR 专用连接（只负责语音识别，不输出音频）
// 3. 实现"文本镜像"策略：让所有连接看起来共享同一段对话
type VoicePool struct {
	sessionID string

	// 角色连接映射：role -> RoleConn
	roleConns   map[string]*RoleConn
	roleConnsMu sync.RWMutex

	// ASR 专用连接（只做语音识别，不输出音频）
	asrConn   *RoleConn
	asrConnMu sync.RWMutex

	// 当前正在说话的角色（用于插话中断）
	speakingRole   string
	speakingRoleMu sync.RWMutex

	// 对话历史（用于文本镜像同步）
	conversationHistory   []ConversationTurn
	conversationHistoryMu sync.RWMutex

	// 配置
	config VoicePoolConfig

	logger *log.Logger
}

// ConversationTurn 对话轮次
type ConversationTurn struct {
	Role     string // "user" or "assistant"
	Text     string
	FromRole string // 对于 assistant，记录是哪个角色说的
}

// VoicePoolConfig 音色池配置
type VoicePoolConfig struct {
	OpenAIAPIKey                 string
	Model                        string
	DefaultInstructions          string
	InputAudioFormat             string
	OutputAudioFormat            string
	InputAudioTranscriptionModel string
	RoleVoices                   map[string]string // role -> voice
}

// NewVoicePool 创建一个新的音色池
func NewVoicePool(sessionID string, config VoicePoolConfig) *VoicePool {
	return &VoicePool{
		sessionID:           sessionID,
		roleConns:           make(map[string]*RoleConn),
		conversationHistory: make([]ConversationTurn, 0),
		config:              config,
		logger:              log.Default(),
	}
}

// Initialize 初始化音色池（只创建 ASR 连接）
func (vp *VoicePool) Initialize(ctx context.Context) error {
	vp.logger.Printf("[VoicePool:%s] Initializing voice pool (on-demand mode)", vp.sessionID)

	// 只创建 ASR 专用连接，角色连接按需创建
	if err := vp.createASRConn(ctx); err != nil {
		vp.logger.Printf("[VoicePool:%s] ❌ Failed to create ASR conn: %v", vp.sessionID, err)
		return fmt.Errorf("create ASR conn: %w", err)
	}
	vp.logger.Printf("[VoicePool:%s] ✅ ASR conn created", vp.sessionID)

	vp.logger.Printf("[VoicePool:%s] ✅ Voice pool initialized (roles will be created on-demand)", vp.sessionID)
	return nil
}

// createRoleConn 创建并初始化一个角色连接
func (vp *VoicePool) createRoleConn(ctx context.Context, role string, voice string) error {
	config := RoleConnConfig{
		OpenAIAPIKey:                 vp.config.OpenAIAPIKey,
		Model:                        vp.config.Model,
		Voice:                        voice,
		Instructions:                 vp.config.DefaultInstructions,
		InputAudioFormat:             vp.config.InputAudioFormat,
		OutputAudioFormat:            vp.config.OutputAudioFormat,
		InputAudioTranscriptionModel: vp.config.InputAudioTranscriptionModel,
		EnableAudioOutput:            true,
	}

	roleConn := NewRoleConn(role, voice, config)

	if err := roleConn.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := roleConn.Initialize(ctx); err != nil {
		_ = roleConn.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	vp.roleConnsMu.Lock()
	vp.roleConns[role] = roleConn
	vp.roleConnsMu.Unlock()

	return nil
}

// createASRConn 创建 ASR 专用连接
func (vp *VoicePool) createASRConn(ctx context.Context) error {
	config := RoleConnConfig{
		OpenAIAPIKey:                 vp.config.OpenAIAPIKey,
		Model:                        vp.config.Model,
		Voice:                        "alloy",
		Instructions:                 vp.config.DefaultInstructions,
		InputAudioFormat:             vp.config.InputAudioFormat,
		OutputAudioFormat:            vp.config.OutputAudioFormat,
		InputAudioTranscriptionModel: vp.config.InputAudioTranscriptionModel,
		EnableAudioOutput:            false,
	}

	asrConn := NewRoleConn("asr", "alloy", config)

	if err := asrConn.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	if err := asrConn.Initialize(ctx); err != nil {
		_ = asrConn.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	vp.asrConnMu.Lock()
	vp.asrConn = asrConn
	vp.asrConnMu.Unlock()

	return nil
}

// GetRoleConn 获取指定角色的连接（按需创建）
func (vp *VoicePool) GetRoleConn(role string) (*RoleConn, error) {
	// 先尝试获取已存在的连接
	vp.roleConnsMu.RLock()
	conn, exists := vp.roleConns[role]
	vp.roleConnsMu.RUnlock()

	if exists {
		return conn, nil
	}

	// 连接不存在，按需创建
	vp.logger.Printf("[VoicePool:%s] Role conn for '%s' not found, creating on-demand...", vp.sessionID, role)

	// 获取该角色的音色配置
	voice, ok := vp.config.RoleVoices[role]
	if !ok {
		return nil, fmt.Errorf("role '%s' not configured in RoleVoices", role)
	}

	// 创建角色连接（需要写锁）
	vp.roleConnsMu.Lock()
	defer vp.roleConnsMu.Unlock()

	// 双重检查：可能在等待锁期间已被其他 goroutine 创建
	if conn, exists := vp.roleConns[role]; exists {
		vp.logger.Printf("[VoicePool:%s] Role conn for '%s' was created by another goroutine", vp.sessionID, role)
		return conn, nil
	}

	// 创建新连接
	ctx := context.Background()
	if err := vp.createRoleConn(ctx, role, voice); err != nil {
		return nil, fmt.Errorf("create role conn on-demand: %w", err)
	}

	vp.logger.Printf("[VoicePool:%s] ✅ Role conn for '%s' (voice=%s) created on-demand", vp.sessionID, role, voice)

	return vp.roleConns[role], nil
}

// GetASRConn 获取 ASR 连接
func (vp *VoicePool) GetASRConn() (*RoleConn, error) {
	vp.asrConnMu.RLock()
	defer vp.asrConnMu.RUnlock()

	if vp.asrConn == nil {
		return nil, fmt.Errorf("ASR conn not initialized")
	}
	return vp.asrConn, nil
}

// SyncUserText 同步用户文本到所有角色连接
func (vp *VoicePool) SyncUserText(text string) error {
	vp.logger.Printf("[VoicePool:%s] Syncing user text to all role conns: %s", vp.sessionID, text)

	vp.conversationHistoryMu.Lock()
	vp.conversationHistory = append(vp.conversationHistory, ConversationTurn{
		Role: "user",
		Text: text,
	})
	vp.conversationHistoryMu.Unlock()

	vp.roleConnsMu.RLock()
	defer vp.roleConnsMu.RUnlock()

	var lastErr error
	for role, conn := range vp.roleConns {
		if err := conn.SyncUserText(text); err != nil {
			vp.logger.Printf("[VoicePool:%s] ⚠️  Failed to sync user text to %s: %v", vp.sessionID, role, err)
			lastErr = err
		}
	}
	return lastErr
}

// SyncAssistantText 同步助手文本到所有角色连接
func (vp *VoicePool) SyncAssistantText(text string, fromRole string) error {
	vp.logger.Printf("[VoicePool:%s] Syncing assistant text from %s to all role conns: %s", vp.sessionID, fromRole, text)

	vp.conversationHistoryMu.Lock()
	vp.conversationHistory = append(vp.conversationHistory, ConversationTurn{
		Role:     "assistant",
		Text:     text,
		FromRole: fromRole,
	})
	vp.conversationHistoryMu.Unlock()

	vp.roleConnsMu.RLock()
	defer vp.roleConnsMu.RUnlock()

	var lastErr error
	for role, conn := range vp.roleConns {
		if err := conn.SyncAssistantText(text, fromRole); err != nil {
			vp.logger.Printf("[VoicePool:%s] ⚠️  Failed to sync assistant text to %s: %v", vp.sessionID, role, err)
			lastErr = err
		}
	}
	return lastErr
}

// CreateResponse 在指定角色连接上创建响应
func (vp *VoicePool) CreateResponse(role string, instructions string, metadata map[string]interface{}) error {
	vp.logger.Printf("[VoicePool:%s] Creating response for role %s", vp.sessionID, role)

	conn, err := vp.GetRoleConn(role)
	if err != nil {
		return fmt.Errorf("get role conn: %w", err)
	}

	vp.speakingRoleMu.Lock()
	vp.speakingRole = role
	vp.speakingRoleMu.Unlock()

	return conn.CreateResponse(instructions, metadata)
}

// CancelCurrentResponse 取消当前正在说话的角色的响应
func (vp *VoicePool) CancelCurrentResponse() error {
	vp.speakingRoleMu.RLock()
	role := vp.speakingRole
	vp.speakingRoleMu.RUnlock()

	if role == "" {
		vp.logger.Printf("[VoicePool:%s] No active speaker to cancel", vp.sessionID)
		return nil
	}

	vp.logger.Printf("[VoicePool:%s] Canceling response for role %s", vp.sessionID, role)

	conn, err := vp.GetRoleConn(role)
	if err != nil {
		return fmt.Errorf("get role conn: %w", err)
	}

	return conn.CancelResponse()
}

// ClearSpeakingRole 清除当前正在说话的角色
func (vp *VoicePool) ClearSpeakingRole() {
	vp.speakingRoleMu.Lock()
	vp.speakingRole = ""
	vp.speakingRoleMu.Unlock()
}

// GetConversationHistory 获取对话历史
func (vp *VoicePool) GetConversationHistory() []ConversationTurn {
	vp.conversationHistoryMu.RLock()
	defer vp.conversationHistoryMu.RUnlock()

	history := make([]ConversationTurn, len(vp.conversationHistory))
	copy(history, vp.conversationHistory)
	return history
}

// Close 关闭所有连接
func (vp *VoicePool) Close() error {
	vp.logger.Printf("[VoicePool:%s] Closing voice pool", vp.sessionID)

	vp.roleConnsMu.Lock()
	for role, conn := range vp.roleConns {
		vp.logger.Printf("[VoicePool:%s] Closing role conn: %s", vp.sessionID, role)
		_ = conn.Close()
	}
	vp.roleConns = make(map[string]*RoleConn)
	vp.roleConnsMu.Unlock()

	vp.asrConnMu.Lock()
	if vp.asrConn != nil {
		vp.logger.Printf("[VoicePool:%s] Closing ASR conn", vp.sessionID)
		_ = vp.asrConn.Close()
		vp.asrConn = nil
	}
	vp.asrConnMu.Unlock()

	vp.logger.Printf("[VoicePool:%s] ✅ Voice pool closed", vp.sessionID)
	return nil
}
