package gateway

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bubble-talk/server/internal/tool"
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

	// 角色连接创建锁（防止重复创建）
	roleConnCreating   map[string]chan struct{}
	roleConnCreatingMu sync.Mutex

	// ASR 专用连接（只做语音识别，不输出音频）
	asrConn   *RoleConn
	asrConnMu sync.RWMutex

	// 当前正在说话的角色（用于插话中断）
	speakingRole   string
	speakingRoleMu sync.RWMutex

	// 对话历史（用于文本镜像同步）
	conversationHistory   []ConversationTurn
	conversationHistoryMu sync.RWMutex

	// 工具注册表
	toolRegistry *tool.ToolRegistry

	// 配置
	config VoicePoolConfig

	logger *log.Logger
}

const roleConnCreateTimeout = 45 * time.Second // 增加到 45 秒，允许 3 次重试

func (vp *VoicePool) logf(format string, args ...any) {
	if vp.logger != nil {
		vp.logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
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
		roleConnCreating:    make(map[string]chan struct{}),
		conversationHistory: make([]ConversationTurn, 0),
		config:              config,
		logger:              log.Default(),
	}
}

// Initialize 初始化音色池（只创建 ASR 连接）
func (vp *VoicePool) Initialize(ctx context.Context) error {
	vp.logf("[VoicePool:%s] Initializing voice pool (on-demand mode)", vp.sessionID)

	// 只创建 ASR 专用连接，角色连接按需创建
	if err := vp.createASRConn(ctx); err != nil {
		vp.logf("[VoicePool:%s] ❌ Failed to create ASR conn: %v", vp.sessionID, err)
		return fmt.Errorf("create ASR conn: %w", err)
	}
	vp.logf("[VoicePool:%s] ✅ ASR conn created", vp.sessionID)

	vp.logf("[VoicePool:%s] ✅ Voice pool initialized (roles will be created on-demand)", vp.sessionID)
	return nil
}

// newRoleConn 创建并初始化一个角色连接（不写入 roleConns；由调用方决定何时注册）
func (vp *VoicePool) newRoleConn(ctx context.Context, role string, voice string) (*RoleConn, error) {
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

	// 设置工具注册表（如果有）
	if vp.toolRegistry != nil {
		roleConn.SetToolRegistry(vp.toolRegistry)
		vp.logf("[VoicePool:%s] Tool registry set for new role conn: %s", vp.sessionID, role)
	}

	if err := roleConn.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	if err := roleConn.Initialize(ctx); err != nil {
		_ = roleConn.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	return roleConn, nil
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

	// 设置工具注册表（如果有）
	if vp.toolRegistry != nil {
		asrConn.SetToolRegistry(vp.toolRegistry)
		vp.logf("[VoicePool:%s] Tool registry set for ASR conn", vp.sessionID)
	}

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
func (vp *VoicePool) GetRoleConn(ctx context.Context, role string) (*RoleConn, error) {
	// 先尝试获取已存在的连接
	vp.roleConnsMu.RLock()
	conn, exists := vp.roleConns[role]
	vp.roleConnsMu.RUnlock()

	if exists {
		return conn, nil
	}

	// 检查是否有其他 goroutine 正在创建
	vp.roleConnCreatingMu.Lock()
	// Ensure the map is initialized to avoid panic when tests or callers construct VoicePool directly
	if vp.roleConnCreating == nil {
		vp.roleConnCreating = make(map[string]chan struct{})
	}
	creatingChan, isCreating := vp.roleConnCreating[role]
	if isCreating {
		// 有其他 goroutine 正在创建，等待它完成
		vp.roleConnCreatingMu.Unlock()
		vp.logf("[VoicePool:%s] Another goroutine is creating role conn for '%s', waiting...", vp.sessionID, role)

		select {
		case <-creatingChan:
			// 创建完成，重新获取
			vp.roleConnsMu.RLock()
			conn, exists := vp.roleConns[role]
			vp.roleConnsMu.RUnlock()
			if exists {
				return conn, nil
			}
			return nil, fmt.Errorf("role conn creation completed but conn not found for '%s'", role)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// 标记正在创建
	creatingChan = make(chan struct{})
	vp.roleConnCreating[role] = creatingChan
	vp.roleConnCreatingMu.Unlock()

	// 确保完成后清理标记
	defer func() {
		vp.roleConnCreatingMu.Lock()
		delete(vp.roleConnCreating, role)
		close(creatingChan)
		vp.roleConnCreatingMu.Unlock()
	}()

	// 连接不存在，按需创建
	vp.logf("[VoicePool:%s] Role conn for '%s' not found, creating on-demand...", vp.sessionID, role)

	// 获取该角色的音色配置
	voice, ok := vp.config.RoleVoices[role]
	if !ok {
		return nil, fmt.Errorf("role '%s' not configured in RoleVoices", role)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, roleConnCreateTimeout)
		defer cancel()
	}

	// 连接建立/初始化涉及网络 I/O，不应持有 roleConnsMu，避免阻塞其它读写路径。
	roleConn, err := vp.newRoleConn(ctx, role, voice)
	if err != nil {
		return nil, fmt.Errorf("create role conn on-demand: %w", err)
	}

	// 注册连接
	vp.roleConnsMu.Lock()
	vp.roleConns[role] = roleConn
	vp.roleConnsMu.Unlock()

	vp.logf("[VoicePool:%s] ✅ Role conn for '%s' (voice=%s) created on-demand", vp.sessionID, role, voice)

	return roleConn, nil
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
	vp.logf("[VoicePool:%s] Syncing user text to all role conns: %s", vp.sessionID, text)

	vp.conversationHistoryMu.Lock()
	vp.conversationHistory = append(vp.conversationHistory, ConversationTurn{
		Role: "user",
		Text: text,
	})
	vp.conversationHistoryMu.Unlock()

	// 遍历配置的所有角色
	var lastErr error
	for role := range vp.config.RoleVoices {
		// 先尝试获取已存在的连接（不创建新的）
		vp.roleConnsMu.RLock()
		conn, exists := vp.roleConns[role]
		vp.roleConnsMu.RUnlock()

		if exists {
			// 连接已存在，直接同步
			if err := conn.SyncUserText(text); err != nil {
				vp.logf("[VoicePool:%s] ⚠️  Failed to sync user text to %s: %v", vp.sessionID, role, err)
				lastErr = err
			}
			vp.logf("[VoicePool:%s] ✅ Synced user text to %s", vp.sessionID, role)
		} else {
			// 连接不存在，异步创建并同步
			vp.logf("[VoicePool:%s] Role %s not found, will create and sync asynchronously", vp.sessionID, role)
			go vp.createAndSyncUserText(role, text)
		}
	}
	return lastErr
}

// createAndSyncUserText 异步创建角色连接并同步用户文本
func (vp *VoicePool) createAndSyncUserText(role string, text string) {
	vp.logf("[VoicePool:%s] Creating role conn for %s asynchronously...", vp.sessionID, role)

	conn, err := vp.GetRoleConn(context.Background(), role)
	if err != nil {
		vp.logf("[VoicePool:%s] ❌ Failed to create role conn for %s: %v", vp.sessionID, role, err)
		return
	}

	// 同步历史记录中的所有用户消息
	vp.conversationHistoryMu.RLock()
	history := make([]ConversationTurn, len(vp.conversationHistory))
	copy(history, vp.conversationHistory)
	vp.conversationHistoryMu.RUnlock()

	vp.logf("[VoicePool:%s] Syncing %d historical messages to %s", vp.sessionID, len(history), role)

	for _, turn := range history {
		if turn.Role == "user" {
			if err := conn.SyncUserText(turn.Text); err != nil {
				vp.logf("[VoicePool:%s] ⚠️  Failed to sync historical user text to %s: %v", vp.sessionID, role, err)
			}
		} else if turn.Role == "assistant" {
			if err := conn.SyncAssistantText(turn.Text, turn.FromRole); err != nil {
				vp.logf("[VoicePool:%s] ⚠️  Failed to sync historical assistant text to %s: %v", vp.sessionID, role, err)
			}
		}
	}

	vp.logf("[VoicePool:%s] ✅ Role %s created and synced with history", vp.sessionID, role)
}

// SyncAssistantText 同步助手文本到所有角色连接
func (vp *VoicePool) SyncAssistantText(text string, fromRole string) error {
	vp.logf("[VoicePool:%s] Syncing assistant text from %s to all role conns: %s", vp.sessionID, fromRole, text)

	vp.conversationHistoryMu.Lock()
	vp.conversationHistory = append(vp.conversationHistory, ConversationTurn{
		Role:     "assistant",
		Text:     text,
		FromRole: fromRole,
	})
	vp.conversationHistoryMu.Unlock()

	// 只同步到已存在的连接，避免阻塞
	vp.roleConnsMu.RLock()
	defer vp.roleConnsMu.RUnlock()

	var lastErr error
	for role, conn := range vp.roleConns {
		// FIX: ASR连接不应该接收assistant消息，它只负责转写用户音频
		if role == "asr" {
			vp.logf("[VoicePool:%s] ⏭️  Skipping ASR conn (it should only transcribe, not receive assistant messages)", vp.sessionID)
			continue
		}

		// 发送者自己不需要收到自己的消息
		if role == fromRole {
			vp.logf("[VoicePool:%s] ⏭️  Skip syncing own message to %s", vp.sessionID, role)
			continue
		}

		if err := conn.SyncAssistantText(text, fromRole); err != nil {
			vp.logf("[VoicePool:%s] ⚠️  Failed to sync assistant text to %s: %v", vp.sessionID, role, err)
			lastErr = err
		} else {
			vp.logf("[VoicePool:%s] ✅ Synced assistant text to %s", vp.sessionID, role)
		}
	}
	return lastErr
}

// CreateResponse 在指定角色连接上创建响应
func (vp *VoicePool) CreateResponse(ctx context.Context, role string, instructions string, metadata map[string]interface{}) error {
	vp.logf("[VoicePool:%s] Creating response for role %s", vp.sessionID, role)

	conn, err := vp.GetRoleConn(ctx, role)
	if err != nil {
		return fmt.Errorf("get role conn: %w", err)
	}

	vp.speakingRoleMu.Lock()
	vp.speakingRole = role
	vp.speakingRoleMu.Unlock()

	// 保存 metadata 到连接，以便 response.created 时可以使用
	conn.SetPendingMetadata(metadata)

	return conn.CreateResponse(instructions, metadata)
}

// CancelCurrentResponse 取消当前正在说话的角色的响应
func (vp *VoicePool) CancelCurrentResponse() error {
	vp.speakingRoleMu.RLock()
	role := vp.speakingRole
	vp.speakingRoleMu.RUnlock()

	if role == "" {
		vp.logf("[VoicePool:%s] No active speaker to cancel", vp.sessionID)
		return nil
	}

	vp.logf("[VoicePool:%s] Canceling response for role %s", vp.sessionID, role)

	conn, err := vp.GetRoleConn(context.Background(), role)
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

// SetToolRegistry 设置工具注册表，并传递给所有已创建的角色连接
func (vp *VoicePool) SetToolRegistry(registry *tool.ToolRegistry) {
	vp.toolRegistry = registry

	// 传递给所有已创建的角色连接
	vp.roleConnsMu.RLock()
	defer vp.roleConnsMu.RUnlock()

	for role, conn := range vp.roleConns {
		if conn != nil {
			conn.SetToolRegistry(registry)
			vp.logf("[VoicePool:%s] Tool registry set for role %s", vp.sessionID, role)
		}
	}

	// 也传递给 ASR 连接（虽然它不会用到，但保持一致性）
	vp.asrConnMu.RLock()
	if vp.asrConn != nil {
		vp.asrConn.SetToolRegistry(registry)
	}
	vp.asrConnMu.RUnlock()
}

// Close 关闭所有连接
func (vp *VoicePool) Close() error {
	vp.logf("[VoicePool:%s] Closing voice pool", vp.sessionID)

	vp.roleConnsMu.Lock()
	for role, conn := range vp.roleConns {
		vp.logf("[VoicePool:%s] Closing role conn: %s", vp.sessionID, role)
		_ = conn.Close()
	}
	vp.roleConns = make(map[string]*RoleConn)
	vp.roleConnsMu.Unlock()

	vp.asrConnMu.Lock()
	if vp.asrConn != nil {
		vp.logf("[VoicePool:%s] Closing ASR conn", vp.sessionID)
		_ = vp.asrConn.Close()
		vp.asrConn = nil
	}
	vp.asrConnMu.Unlock()

	vp.logf("[VoicePool:%s] ✅ Voice pool closed", vp.sessionID)
	return nil
}
