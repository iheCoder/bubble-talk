package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"bubble-talk/server/internal/tool"

	"github.com/gorilla/websocket"
)

// RoleConn 代表一个特定角色的 Realtime 连接
// 每个 RoleConn 在初始化时固定一个 voice，之后不再变化
type RoleConn struct {
	role  string // 角色名称（如 "host", "economist", "skeptic"）
	voice string // 固定的音色（如 "alloy", "echo", "shimmer"）

	conn     *websocket.Conn
	connLock sync.Mutex

	// 当前活跃的响应ID（用于插话中断）
	activeResponseID     string
	activeResponseIDLock sync.RWMutex

	// 工具注册表
	toolRegistry *tool.ToolRegistry

	// 连接状态
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// 配置
	config RoleConnConfig

	logger *log.Logger
}

// RoleConnConfig 单个角色连接的配置
type RoleConnConfig struct {
	OpenAIAPIKey                 string
	Model                        string
	Voice                        string
	Instructions                 string
	InputAudioFormat             string
	OutputAudioFormat            string
	InputAudioTranscriptionModel string
	EnableAudioOutput            bool // 是否启用音频输出（ASR连接应设为false）
}

// NewRoleConn 创建一个新的角色连接
func NewRoleConn(role string, voice string, config RoleConnConfig) *RoleConn {
	ctx, cancel := context.WithCancel(context.Background())

	return &RoleConn{
		role:      role,
		voice:     voice,
		conn:      nil,
		ctx:       ctx,
		cancel:    cancel,
		closeChan: make(chan struct{}),
		config:    config,
		logger:    log.Default(),
	}
}

// Connect 连接到 OpenAI Realtime API（带重试）
func (rc *RoleConn) Connect(ctx context.Context) error {
	url := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", rc.config.Model)
	if rc.config.Model == "" {
		url = "wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-12-17"
	}

	rc.logger.Printf("[RoleConn:%s] Connecting to: %s with voice: %s", rc.role, url, rc.voice)

	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + rc.config.OpenAIAPIKey}
	headers["OpenAI-Beta"] = []string{"realtime=v1"}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	// 重试机制：最多 3 次，处理 EOF 等临时错误
	var conn *websocket.Conn
	var resp *http.Response
	var err error

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		conn, resp, err = dialer.DialContext(ctx, url, headers)
		if err == nil {
			// 连接成功
			break
		}

		// 记录错误详情
		if resp != nil {
			rc.logger.Printf("[RoleConn:%s] ⚠️ Dial attempt %d/%d failed: HTTP %d", rc.role, attempt, maxRetries, resp.StatusCode)
		} else {
			rc.logger.Printf("[RoleConn:%s] ⚠️ Dial attempt %d/%d failed: %v", rc.role, attempt, maxRetries, err)
		}

		// 最后一次尝试，不再重试
		if attempt == maxRetries {
			if resp != nil {
				return fmt.Errorf("dial realtime: status=%d err=%w", resp.StatusCode, err)
			}
			return fmt.Errorf("dial realtime: %w", err)
		}

		// 指数退避：300ms, 1s, 3s
		backoff := time.Duration(300*attempt*attempt) * time.Millisecond
		rc.logger.Printf("[RoleConn:%s] Retrying in %v...", rc.role, backoff)

		select {
		case <-time.After(backoff):
			// 继续重试
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	rc.connLock.Lock()
	rc.conn = conn
	rc.connLock.Unlock()

	rc.logger.Printf("[RoleConn:%s] ✅ Connected successfully", rc.role)
	return nil
}

// Initialize 初始化会话配置（固定 voice）
func (rc *RoleConn) Initialize(ctx context.Context) error {
	// 构造 session.update 指令，固定 voice
	sessionConfig := RealtimeSessionConfig{
		Modalities:        []string{"text", "audio"},
		Instructions:      rc.config.Instructions,
		Voice:             rc.voice, // 固定音色，之后不再改变
		InputAudioFormat:  rc.config.InputAudioFormat,
		OutputAudioFormat: rc.config.OutputAudioFormat,
		InputAudioTranscription: &InputAudioTranscriptionConfig{
			Model: rc.config.InputAudioTranscriptionModel,
		},
		TurnDetection: &TurnDetectionConfig{
			Type:              "server_vad",
			Threshold:         0.5,
			PrefixPaddingMS:   300,
			SilenceDurationMS: 500,
			CreateResponse:    false, // 禁用自动响应，由我们控制
		},
		Temperature: 0.8,
	}

	// 如果有工具注册表，添加工具定义
	if rc.toolRegistry != nil {
		toolDefs := rc.toolRegistry.GetAllDefinitions()
		if len(toolDefs) > 0 {
			tools := make([]interface{}, len(toolDefs))
			for i, def := range toolDefs {
				tools[i] = def
			}
			sessionConfig.Tools = tools
			rc.logger.Printf("[RoleConn:%s] 🔧 Registered %d tools to session", rc.role, len(toolDefs))
		}
	}

	update := RealtimeSessionUpdate{
		Type:    "session.update",
		Session: sessionConfig,
	}

	// 如果是 ASR 专用连接，禁用音频输出
	if !rc.config.EnableAudioOutput {
		update.Session.Modalities = []string{"text"}
	}

	// 设置默认值
	if rc.config.InputAudioFormat == "" {
		update.Session.InputAudioFormat = "pcm16"
	}
	if rc.config.OutputAudioFormat == "" {
		update.Session.OutputAudioFormat = "pcm16"
	}
	if update.Session.InputAudioTranscription != nil && update.Session.InputAudioTranscription.Model == "" {
		update.Session.InputAudioTranscription.Model = "whisper-1"
	}

	rc.logger.Printf("[RoleConn:%s] Initializing session with voice=%s", rc.role, rc.voice)

	if err := rc.SendMessage(update); err != nil {
		rc.logger.Printf("[RoleConn:%s] ❌ Failed to send session.update: %v", rc.role, err)
		return err
	}

	rc.logger.Printf("[RoleConn:%s] ✅ Session initialized", rc.role)
	return nil
}

// SendMessage 发送消息到 OpenAI Realtime
func (rc *RoleConn) SendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	rc.connLock.Lock()
	defer rc.connLock.Unlock()

	if rc.conn == nil {
		return fmt.Errorf("connection not established")
	}

	if err := rc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	return nil
}

// ReadMessage 从 OpenAI Realtime 读取消息
func (rc *RoleConn) ReadMessage() (int, []byte, error) {
	rc.connLock.Lock()
	conn := rc.conn
	rc.connLock.Unlock()

	if conn == nil {
		return 0, nil, fmt.Errorf("connection not established")
	}

	return conn.ReadMessage()
}

// SyncUserText 同步用户文本到该连接的对话历史
// 这是实现"共享对话"的关键：所有角色连接都会收到用户说了什么
func (rc *RoleConn) SyncUserText(text string) error {
	item := RealtimeConversationItemCreate{
		Type: "conversation.item.create",
		Item: RealtimeConversationItem{
			Type: "message",
			Role: "user",
			Content: []RealtimeContentPart{
				{
					Type: "input_text",
					Text: text,
				},
			},
		},
	}

	rc.logger.Printf("[RoleConn:%s] Syncing user text: %s", rc.role, text)
	return rc.SendMessage(item)
}

// SyncAssistantText 同步助手文本到该连接的对话历史
// 这确保所有角色都能"看到"其他角色说了什么
func (rc *RoleConn) SyncAssistantText(text string, fromRole string) error {
	// 如果是自己说的，不需要同步（已经在对话历史中了）
	if fromRole == rc.role {
		rc.logger.Printf("[RoleConn:%s] Skip syncing own message", rc.role)
		return nil
	}

	item := RealtimeConversationItemCreate{
		Type: "conversation.item.create",
		Item: RealtimeConversationItem{
			Type: "message",
			Role: "assistant",
			Content: []RealtimeContentPart{
				{
					Type: "text",
					Text: text,
				},
			},
		},
	}

	rc.logger.Printf("[RoleConn:%s] Syncing assistant text from %s: %s", rc.role, fromRole, text)
	return rc.SendMessage(item)
}

// CreateResponse 在该连接上创建响应（生成该角色的语音）
func (rc *RoleConn) CreateResponse(instructions string, metadata map[string]interface{}) error {
	create := RealtimeResponseCreate{
		Type: "response.create",
		Response: RealtimeResponseCreateConfig{
			Modalities:   []string{"text", "audio"},
			Instructions: instructions,
			// 注意：这里不设置 voice，因为已经在 session.update 时固定了
			Temperature: 0.8,
			Metadata:    metadata,
		},
	}

	rc.logger.Printf("[RoleConn:%s] Creating response with instructions (len=%d)", rc.role, len(instructions))
	return rc.SendMessage(create)
}

// CancelResponse 取消当前活跃的响应（用于插话中断）
func (rc *RoleConn) CancelResponse() error {
	rc.activeResponseIDLock.RLock()
	responseID := rc.activeResponseID
	rc.activeResponseIDLock.RUnlock()

	if responseID == "" {
		rc.logger.Printf("[RoleConn:%s] No active response to cancel", rc.role)
		return nil
	}

	cancel := RealtimeResponseCancel{
		Type:       "response.cancel",
		ResponseID: responseID,
	}

	rc.logger.Printf("[RoleConn:%s] Canceling response: %s", rc.role, responseID)
	return rc.SendMessage(cancel)
}

// SetActiveResponse 设置当前活跃的响应ID
func (rc *RoleConn) SetActiveResponse(responseID string) {
	rc.activeResponseIDLock.Lock()
	rc.activeResponseID = responseID
	rc.activeResponseIDLock.Unlock()
}

// ClearActiveResponse 清除当前活跃的响应ID
func (rc *RoleConn) ClearActiveResponse() {
	rc.activeResponseIDLock.Lock()
	rc.activeResponseID = ""
	rc.activeResponseIDLock.Unlock()
}

// SetToolRegistry 设置工具注册表
func (rc *RoleConn) SetToolRegistry(registry *tool.ToolRegistry) {
	rc.toolRegistry = registry
	rc.logger.Printf("[RoleConn:%s] Tool registry set", rc.role)
}

// Close 关闭连接
func (rc *RoleConn) Close() error {
	rc.logger.Printf("[RoleConn:%s] Closing connection", rc.role)

	rc.closeOnce.Do(func() {
		rc.cancel()
		close(rc.closeChan)

		rc.connLock.Lock()
		if rc.conn != nil {
			_ = rc.conn.Close()
			rc.conn = nil
		}
		rc.connLock.Unlock()
	})

	return nil
}

// Done 返回一个在连接关闭时关闭的 channel
func (rc *RoleConn) Done() <-chan struct{} {
	return rc.closeChan
}
