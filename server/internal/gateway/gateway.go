package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"bubble-talk/server/internal/tool"

	"github.com/gorilla/websocket"
)

// EventHandler 处理来自网关的事件（给Orchestrator用）
// 返回error表示处理失败，网关会记录但继续运行
type EventHandler func(ctx context.Context, event *ClientMessage) error

// InstructionSender 发送指令到OpenAI Realtime（由网关调用，Orchestrator实现）
// 这个接口让Orchestrator能够控制Realtime的行为
type InstructionSender interface {
	// SendInstructions 发送导演生成的指令到Realtime
	SendInstructions(ctx context.Context, instructions string, metadata map[string]interface{}) error
}

// Gateway 是Realtime语音网关的核心
// 职责：
// 1. 维护客户端↔后端的WebSocket连接（会话通道）
// 2. 维护后端↔OpenAI Realtime的WebSocket连接（语音能力）
// 3. 路由事件：客户端事件→Orchestrator，Orchestrator指令→OpenAI
// 4. 处理插话中断（barge-in）
// 5. 转发音频流（双向）
type Gateway struct {
	sessionID string

	// 客户端连接
	clientConn     *websocket.Conn
	clientConnLock sync.Mutex

	// OpenAI Realtime连接
	realtimeConn     *websocket.Conn
	realtimeConnLock sync.Mutex

	// 事件处理器（由Orchestrator注入）
	eventHandler EventHandler

	// 工具注册表（支持function calling）
	toolRegistry *tool.ToolRegistry

	// 状态管理
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// 当前活跃的响应ID（用于barge-in取消）
	activeResponseID     string
	activeResponseIDLock sync.RWMutex

	// 当前响应的元数据（角色、Beat等）
	activeMetadata     map[string]interface{}
	activeMetadataLock sync.RWMutex

	// response.create 的标记字段，用于区分“我们创建的 response”与“Realtime 自动创建(若存在)”
	responseCreateNonce      int64
	responseCreateNonceLock  sync.Mutex
	lastResponseCreateAt     time.Time
	lastResponseCreateAtLock sync.Mutex

	// 序列号生成器（用于ServerMessage）
	seqCounter int64
	seqLock    sync.Mutex

	// 配置
	config GatewayConfig

	// 日志（可选，生产环境替换为结构化日志）
	logger *log.Logger
}

// GatewayConfig 网关配置
type GatewayConfig struct {
	// OpenAI Realtime配置
	OpenAIAPIKey      string
	OpenAIRealtimeURL string // wss://api.openai.com/v1/realtime?model=gpt-realtime-2025-08-28
	Model             string
	Voice             string
	RoleProfiles      map[string]RoleProfile

	// 默认指令（基础人设）
	DefaultInstructions string

	// 超时配置
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration

	// 音频配置
	InputAudioFormat             string // pcm16
	OutputAudioFormat            string // pcm16
	InputAudioTranscriptionModel string
}

type RoleProfile struct {
	Voice  string
	Avatar string
}

// NewGateway 创建一个新的Gateway实例
func NewGateway(sessionID string, clientConn *websocket.Conn, config GatewayConfig) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())

	return &Gateway{
		sessionID:  sessionID,
		clientConn: clientConn,
		ctx:        ctx,
		cancel:     cancel,
		closeChan:  make(chan struct{}),
		config:     config,
		logger:     log.Default(),
	}
}

// SetEventHandler 设置事件处理器（Orchestrator注入）
func (g *Gateway) SetEventHandler(handler EventHandler) {
	g.eventHandler = handler
}

// Start 启动网关（核心生命周期）
// 步骤：
// 1. 连接OpenAI Realtime
// 2. 初始化会话配置
// 3. 启动双向转发协程
func (g *Gateway) Start(ctx context.Context) error {
	g.logger.Printf("[Gateway] Starting gateway for session %s", g.sessionID)
	g.logger.Printf("[Gateway] Config: model=%s voice=%s input_format=%s output_format=%s",
		g.config.Model, g.config.Voice, g.config.InputAudioFormat, g.config.OutputAudioFormat)

	// 1. 连接OpenAI Realtime
	g.logger.Printf("[Gateway] Connecting to OpenAI Realtime...")
	if err := g.connectRealtime(ctx); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to connect to OpenAI Realtime: %v", err)
		return fmt.Errorf("connect realtime: %w", err)
	}
	g.logger.Printf("[Gateway] ✅ Successfully connected to OpenAI Realtime")

	// 2. 初始化会话配置
	g.logger.Printf("[Gateway] Initializing Realtime session...")
	if err := g.initRealtimeSession(ctx); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to initialize session: %v", err)
		_ = g.closeRealtimeConn()
		return fmt.Errorf("init realtime session: %w", err)
	}
	g.logger.Printf("[Gateway] ✅ Realtime session initialized")

	// 3. 启动事件循环
	g.logger.Printf("[Gateway] Starting event loops...")
	go g.clientReadLoop()
	go g.realtimeReadLoop()
	go g.pingLoop()

	g.logger.Printf("[Gateway] ✅ Gateway fully started for session %s", g.sessionID)
	return nil
}

// connectRealtime 连接到OpenAI Realtime API
func (g *Gateway) connectRealtime(ctx context.Context) error {
	url := g.config.OpenAIRealtimeURL
	if url == "" {
		model := g.config.Model
		if model == "" {
			model = "gpt-realtime-2025-08-28"
		}
		url = fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", model)
	}

	g.logger.Printf("[Gateway] Connecting to: %s", url)
	g.logger.Printf("[Gateway] API Key prefix: %s...", g.config.OpenAIAPIKey[:min(10, len(g.config.OpenAIAPIKey))])

	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + g.config.OpenAIAPIKey}
	headers["OpenAI-Beta"] = []string{"realtime=v1"}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	g.logger.Printf("[Gateway] Dialing WebSocket...")
	conn, resp, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		if resp != nil {
			g.logger.Printf("[Gateway] ❌ Dial failed: HTTP %d %s", resp.StatusCode, resp.Status)
			return fmt.Errorf("dial realtime: status=%d err=%w", resp.StatusCode, err)
		}
		g.logger.Printf("[Gateway] ❌ Dial failed: %v", err)
		return fmt.Errorf("dial realtime: %w", err)
	}

	g.realtimeConn = conn
	g.logger.Printf("[Gateway] ✅ WebSocket connection established")
	g.logger.Printf("[Gateway] Connected to OpenAI Realtime: %s", url)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// initRealtimeSession 初始化Realtime会话配置
func (g *Gateway) initRealtimeSession(_ context.Context) error {
	// 构造session.update指令
	// 策略调整：启用 server_vad 用于自动转写
	// 但我们会在收到转写后立即取消自动响应，改用我们的 Director/Actor
	update := RealtimeSessionUpdate{
		Type: "session.update",
		Session: RealtimeSessionConfig{
			Modalities:        []string{"text", "audio"},
			Instructions:      g.config.DefaultInstructions,
			Voice:             g.defaultVoice(),
			InputAudioFormat:  g.config.InputAudioFormat,
			OutputAudioFormat: g.config.OutputAudioFormat,
			InputAudioTranscription: &InputAudioTranscriptionConfig{
				Model: g.config.InputAudioTranscriptionModel,
			},
			TurnDetection: &TurnDetectionConfig{
				Type:              "server_vad",
				Threshold:         0.5,
				PrefixPaddingMS:   300,
				SilenceDurationMS: 500, // 500ms静音认为说完
				CreateResponse:    false,
			},
			Temperature: 0.8,
		},
	}

	if g.config.InputAudioFormat == "" {
		update.Session.InputAudioFormat = "pcm16"
	}
	if g.config.OutputAudioFormat == "" {
		update.Session.OutputAudioFormat = "pcm16"
	}
	if update.Session.InputAudioTranscription != nil && update.Session.InputAudioTranscription.Model == "" {
		update.Session.InputAudioTranscription.Model = "gpt-4o-mini-transcribe"
	}

	g.logger.Printf("[Gateway] Sending session.update: voice=%s input_format=%s output_format=%s",
		update.Session.Voice, update.Session.InputAudioFormat, update.Session.OutputAudioFormat)
	g.logger.Printf("[Gateway] Instructions length: %d chars", len(update.Session.Instructions))

	if err := g.sendToRealtime(update); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to send session.update: %v", err)
		return err
	}

	g.logger.Printf("[Gateway] ✅ session.update sent successfully")
	return nil
}

// clientReadLoop 从客户端读取消息（事件+音频）
func (g *Gateway) clientReadLoop() {
	defer g.Close()

	for {
		select {
		case <-g.closeChan:
			return
		default:
		}

		messageType, data, err := g.clientConn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				g.logger.Printf("[Gateway] client read error: %v", err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			// JSON事件（quiz_answer/barge_in/exit_requested等）
			if err := g.handleClientEvent(data); err != nil {
				g.logger.Printf("[Gateway] handle client event error: %v", err)
				// 发送错误给客户端，但不断开连接
				g.sendErrorToClient(err.Error())
			}
		} else if messageType == websocket.BinaryMessage {
			// 音频数据（直接转发到Realtime）
			if err := g.handleClientAudio(data); err != nil {
				g.logger.Printf("[Gateway] handle client audio error: %v", err)
			}
		}
	}
}

// handleClientEvent 处理客户端JSON事件
func (g *Gateway) handleClientEvent(data []byte) error {
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal client message: %w", err)
	}

	// 补充服务端时间戳
	if msg.ClientTS.IsZero() {
		msg.ClientTS = time.Now()
	}

	g.logger.Printf("[Gateway] client event: type=%s event_id=%s", msg.Type, msg.EventID)

	// 特殊事件处理
	switch msg.Type {
	case EventTypeBargeIn:
		// 插话中断：立即取消当前TTS
		return g.handleBargeIn(&msg)
	case EventTypeExitRequested:
		// 退出请求：转发给Orchestrator
		return g.forwardToOrchestrator(&msg)
	case EventTypeQuizAnswer:
		// 答题：转发给Orchestrator
		return g.forwardToOrchestrator(&msg)
	default:
		// 其他事件：转发给Orchestrator
		return g.forwardToOrchestrator(&msg)
	}
}

// handleClientAudio 处理客户端音频数据
func (g *Gateway) handleClientAudio(audioData []byte) error {
	// 将音频数据转发到OpenAI Realtime
	// OpenAI期望Base64编码的音频
	encoded := base64.StdEncoding.EncodeToString(audioData)

	append := RealtimeInputAudioBufferAppend{
		Type:  "input_audio_buffer.append",
		Audio: encoded,
	}

	return g.sendToRealtime(append)
}

// handleBargeIn 处理插话中断
func (g *Gateway) handleBargeIn(msg *ClientMessage) error {
	g.logger.Printf("[Gateway] barge-in detected, canceling active response")

	// 1. 取消当前Realtime响应
	g.activeResponseIDLock.RLock()
	responseID := g.activeResponseID
	g.activeResponseIDLock.RUnlock()

	if responseID != "" {
		cancel := RealtimeResponseCancel{
			Type:       "response.cancel",
			ResponseID: responseID,
		}
		if err := g.sendToRealtime(cancel); err != nil {
			g.logger.Printf("[Gateway] failed to cancel response: %v", err)
		}
	}

	// 2. 通知客户端清空音频缓冲区
	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSInterrupted,
		ServerTS: time.Now(),
	})

	// 3. 转发barge_in事件给Orchestrator（用于导演决策）
	return g.forwardToOrchestrator(msg)
}

// forwardToOrchestrator 转发事件给Orchestrator
func (g *Gateway) forwardToOrchestrator(msg *ClientMessage) error {
	if g.eventHandler == nil {
		g.logger.Printf("[Gateway] no event handler set, dropping event: %s", msg.Type)
		return nil
	}

	// 异步调用，避免阻塞读取循环
	go func() {
		ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
		defer cancel()

		if err := g.eventHandler(ctx, msg); err != nil {
			g.logger.Printf("[Gateway] orchestrator handler error: %v", err)
		}
	}()

	return nil
}

// realtimeReadLoop 从OpenAI Realtime读取消息
func (g *Gateway) realtimeReadLoop() {
	defer g.Close()

	for {
		select {
		case <-g.closeChan:
			return
		default:
		}

		messageType, data, err := g.realtimeConn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				g.logger.Printf("[Gateway] realtime read error: %v", err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			// Realtime事件（转写、TTS等）
			if err := g.handleRealtimeEvent(data); err != nil {
				g.logger.Printf("[Gateway] handle realtime event error: %v", err)
			}
		}
		// OpenAI Realtime不使用Binary帧，音频在JSON事件的delta字段中
	}
}

// handleRealtimeEvent 处理OpenAI Realtime事件
func (g *Gateway) handleRealtimeEvent(data []byte) error {
	// 先解析event type
	var base struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return fmt.Errorf("unmarshal realtime event: %w", err)
	}

	g.logger.Printf("[Gateway] realtime event: type=%s event_id=%s", base.Type, base.EventID)

	// 根据事件类型处理
	switch base.Type {
	case "session.created", "session.updated":
		// 会话创建/更新确认，记录日志即可
		return nil

	case "input_audio_buffer.speech_started":
		// 用户开始说话（VAD检测到）
		return g.handleSpeechStarted(data)

	case "input_audio_buffer.speech_stopped":
		// 用户停止说话
		return g.handleSpeechStopped(data)

	case "conversation.item.created":
		// 对话项创建（包含ASR转写）
		return g.handleConversationItemCreated(data)

	case "conversation.item.input_audio_transcription.delta":
		// 输入音频转写增量
		return g.handleInputAudioTranscriptionDelta(data)

	case "conversation.item.input_audio_transcription.completed":
		// 输入音频转写完成
		return g.handleInputAudioTranscriptionCompleted(data)

	case "response.created":
		// 响应创建
		return g.handleResponseCreated(data)

	case "response.output_item.added":
		// 输出项添加
		return g.handleResponseOutputItemAdded(data)

	case "response.content_part.added":
		// 内容部分添加
		return nil
	case "response.content_part.done":
		// 内容部分结束（当前不需要处理，避免日志噪音）
		return nil

	case "response.audio.delta":
		// TTS音频流（转发给客户端）
		return g.handleAudioDelta(data)

	case "response.audio.done":
		// TTS完成
		return g.handleAudioDone(data)

	case "response.audio_transcript.delta", "response.audio_transcript.done":
		// 音频字幕（可选），当前前端不消费，忽略即可
		return nil

	case "response.done":
		// 响应完成
		return g.handleResponseDone(data)

	case "response.text.delta":
		// 文本流（可选，用于字幕）
		return g.handleTextDelta(data)

	case "response.text.done":
		// 文本完成
		return g.handleTextDone(data)

	case "response.output_item.done":
		// 输出项结束（当前不需要处理）
		return nil

	case "response.function_call_arguments.delta":
		// Function call arguments streaming
		return g.handleFunctionCallArgumentsDelta(data)

	case "response.function_call_arguments.done":
		// Function call arguments完成
		return g.handleFunctionCallArgumentsDone(data)

	case "error":
		// 错误事件
		return g.handleRealtimeError(data)

	default:
		// 未知事件，记录但不处理
		g.logger.Printf("[Gateway] unhandled realtime event: %s", base.Type)
		return nil
	}
}

// handleSpeechStarted 处理用户开始说话事件
func (g *Gateway) handleSpeechStarted(data []byte) error {
	// 通知客户端（可选，用于UI反馈）
	g.sendToClient(&ServerMessage{
		Type:     "speech_started",
		ServerTS: time.Now(),
	})
	return nil
}

// handleSpeechStopped 处理用户停止说话事件
func (g *Gateway) handleSpeechStopped(_ []byte) error {
	g.logger.Printf("[Gateway] 🎤 User stopped speaking (VAD detected)")

	// 通知客户端
	_ = g.sendToClient(&ServerMessage{
		Type:     "speech_stopped",
		ServerTS: time.Now(),
	})

	// server_vad 会自动 commit 并生成转写
	// 我们只需要等待 conversation.item.created 事件
	g.logger.Printf("[Gateway] Waiting for automatic transcription from server_vad...")
	return nil
}

// handleConversationItemCreated 处理对话项创建事件（包含ASR转写）
func (g *Gateway) handleConversationItemCreated(data []byte) error {
	var event struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
		Item    struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type       string `json:"type"`
				Text       string `json:"text,omitempty"`
				Transcript string `json:"transcript,omitempty"`
			} `json:"content"`
		} `json:"item"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	g.logger.Printf("[Gateway] 📝 Conversation item created: role=%s type=%s content_count=%d",
		event.Item.Role, event.Item.Type, len(event.Item.Content))

	// 如果是用户消息，提取转写并触发我们的流程
	if event.Item.Role == "user" {
		g.logger.Printf("[Gateway] 👤 User message detected, checking for transcript...")

		if len(event.Item.Content) > 0 {
			for i, content := range event.Item.Content {
				g.logger.Printf("[Gateway]   Content[%d]: type=%s, transcript=%q, text=%q",
					i, content.Type, content.Transcript, content.Text)

				// 尝试从 transcript 或 text 字段获取文本
				transcriptText := content.Transcript
				if transcriptText == "" {
					transcriptText = content.Text
				}

				if transcriptText != "" {
					g.logger.Printf("[Gateway] ✅ Got ASR transcription: %q", transcriptText)

					// 关键：取消即将自动生成的响应
					// server_vad 会自动触发 response.create，我们需要取消它
					g.logger.Printf("[Gateway] 🛑 Canceling auto-generated response to use our Director/Actor...")

					// 这是ASR最终转写，发送给Orchestrator
					asrMsg := &ClientMessage{
						Type:     EventTypeASRFinal,
						Text:     transcriptText,
						TurnID:   event.Item.ID,
						ClientTS: time.Now(),
					}

					// 转发给Orchestrator（这会触发我们的 Director/Actor）
					if err := g.forwardToOrchestrator(asrMsg); err != nil {
						g.logger.Printf("[Gateway] ❌ Failed to forward to Orchestrator: %v", err)
						return err
					}

					// 也发送给客户端（用于UI显示）
					_ = g.sendToClient(&ServerMessage{
						Type:     EventTypeASRFinal,
						Text:     transcriptText,
						TurnID:   event.Item.ID,
						ServerTS: time.Now(),
					})

					g.logger.Printf("[Gateway] ✅ ASR forwarded to Orchestrator")
					return nil
				}
			}
			g.logger.Printf("[Gateway] ⚠️  No transcript found in user message content")
		} else {
			g.logger.Printf("[Gateway] ⚠️  User message has no content")
		}
	}

	return nil
}

// handleInputAudioTranscriptionDelta 处理输入音频转写增量
func (g *Gateway) handleInputAudioTranscriptionDelta(data []byte) error {
	var event struct {
		Type         string `json:"type"`
		EventID      string `json:"event_id"`
		ItemID       string `json:"item_id"`
		ContentIndex int    `json:"content_index"`
		Delta        string `json:"delta"`
		Transcript   string `json:"transcript"`
		Text         string `json:"text"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	text := firstNonEmpty(event.Delta, event.Transcript, event.Text)
	if text == "" {
		return nil
	}

	_ = g.sendToClient(&ServerMessage{
		Type:     EventTypeASRPartial,
		Text:     text,
		TurnID:   event.ItemID,
		ServerTS: time.Now(),
	})
	return nil
}

// handleInputAudioTranscriptionCompleted 处理输入音频转写完成
func (g *Gateway) handleInputAudioTranscriptionCompleted(data []byte) error {
	var event struct {
		Type         string `json:"type"`
		EventID      string `json:"event_id"`
		ItemID       string `json:"item_id"`
		ContentIndex int    `json:"content_index"`
		Transcript   string `json:"transcript"`
		Text         string `json:"text"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	text := firstNonEmpty(event.Transcript, event.Text)
	if text == "" {
		g.logger.Printf("[Gateway] ⚠️  Empty transcription in completed event")
		return nil
	}

	g.logger.Printf("[Gateway] ✅ Got ASR transcription (completed): %q", text)

	asrMsg := &ClientMessage{
		Type:     EventTypeASRFinal,
		Text:     text,
		TurnID:   event.ItemID,
		ClientTS: time.Now(),
	}

	if err := g.forwardToOrchestrator(asrMsg); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to forward to Orchestrator: %v", err)
		return err
	}

	_ = g.sendToClient(&ServerMessage{
		Type:     EventTypeASRFinal,
		Text:     text,
		TurnID:   event.ItemID,
		ServerTS: time.Now(),
	})

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// handleResponseCreated 处理响应创建事件
func (g *Gateway) handleResponseCreated(data []byte) error {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			ID       string                 `json:"id"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"response"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	// 兼容性策略：
	// 1) 优先用 metadata 判断（最稳）
	// 2) 若服务端不回传 metadata，则使用“最近是否发送过 response.create”的时间窗兜底
	// 3) 只有在 1) 不是我们 + 2) 也不满足，才取消，避免误杀我们自己的 response
	if g.isOurRealtimeResponse(event.Response.Metadata) || g.isLikelyOurResponseByRecentCreate() {
		g.logger.Printf("[Gateway] ✅ Our response created: %s", event.Response.ID)
		g.activeResponseIDLock.Lock()
		g.activeResponseID = event.Response.ID
		g.activeResponseIDLock.Unlock()
		return nil
	}

	g.logger.Printf("[Gateway] 🛑 Detected auto-generated response %s, canceling...", event.Response.ID)
	cancel := RealtimeResponseCancel{Type: "response.cancel", ResponseID: event.Response.ID}
	if err := g.sendToRealtime(cancel); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to cancel auto response: %v", err)
	} else {
		g.logger.Printf("[Gateway] ✅ Auto-generated response canceled")
	}
	return nil
}

// handleResponseOutputItemAdded 处理输出项添加事件
func (g *Gateway) handleResponseOutputItemAdded(data []byte) error {
	// TTS开始（附带元数据，便于前端提前切换 activeRole / 动画）
	g.activeMetadataLock.RLock()
	metadata := make(map[string]interface{}, len(g.activeMetadata))
	for k, v := range g.activeMetadata {
		metadata[k] = v
	}
	g.activeMetadataLock.RUnlock()

	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSStarted,
		Metadata: metadata,
		ServerTS: time.Now(),
	})
	return nil
}

// handleAudioDelta 处理TTS音频流
func (g *Gateway) handleAudioDelta(data []byte) error {
	var event struct {
		Type         string `json:"type"`
		ResponseID   string `json:"response_id"`
		ItemID       string `json:"item_id"`
		OutputIndex  int    `json:"output_index"`
		ContentIndex int    `json:"content_index"`
		Delta        string `json:"delta"` // Base64编码的音频
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	// 解码音频
	audioData, err := base64.StdEncoding.DecodeString(event.Delta)
	if err != nil {
		return fmt.Errorf("decode audio delta: %w", err)
	}

	// 转发音频给客户端（Binary帧）
	g.clientConnLock.Lock()
	defer g.clientConnLock.Unlock()

	if err := g.clientConn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
		return fmt.Errorf("send audio to client: %w", err)
	}

	return nil
}

// handleAudioDone 处理TTS完成事件
func (g *Gateway) handleAudioDone(data []byte) error {
	// 通知客户端TTS完成（附带元数据，前端可用于收尾但不应直接等同于“播放已结束”）
	g.activeMetadataLock.RLock()
	metadata := make(map[string]interface{}, len(g.activeMetadata))
	for k, v := range g.activeMetadata {
		metadata[k] = v
	}
	g.activeMetadataLock.RUnlock()

	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSCompleted,
		Metadata: metadata,
		ServerTS: time.Now(),
	})
	return nil
}

// handleResponseDone 处理响应完成事件
func (g *Gateway) handleResponseDone(data []byte) error {
	// 解析完整的 response.done 事件
	var event struct {
		Type       string `json:"type"`
		EventID    string `json:"event_id"`
		ResponseID string `json:"response_id"`
		Response   struct {
			ID            string        `json:"id"`
			Object        string        `json:"object"`
			Status        string        `json:"status"` // "completed", "cancelled", "failed", "incomplete"
			StatusDetails interface{}   `json:"status_details"`
			Output        []interface{} `json:"output"`
			Usage         interface{}   `json:"usage"`
		} `json:"response"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		g.logger.Printf("[Gateway] ⚠️  Failed to parse response.done: %v", err)
		// 继续执行清理逻辑
	} else {
		// 记录详细信息
		outputCount := len(event.Response.Output)
		g.logger.Printf("[Gateway] response.done: id=%s status=%s output_count=%d",
			event.ResponseID, event.Response.Status, outputCount)

		// 检测异常状态
		if event.Response.Status != "completed" {
			g.logger.Printf("[Gateway] ⚠️  Abnormal response status: %s, details: %+v",
				event.Response.Status, event.Response.StatusDetails)
		}

		// 检测空响应（没有生成任何输出）
		if outputCount == 0 {
			g.logger.Printf("[Gateway] ⚠️  Empty response detected (no output items generated)")
			g.logger.Printf("[Gateway] Response details: %s", string(data))
		}
	}

	// 清除活跃响应ID
	g.activeResponseIDLock.Lock()
	g.activeResponseID = ""
	g.activeResponseIDLock.Unlock()

	return nil
}

// handleTextDelta 处理文本流（用于字幕）
func (g *Gateway) handleTextDelta(data []byte) error {
	var event struct {
		Type         string `json:"type"`
		ResponseID   string `json:"response_id"`
		ItemID       string `json:"item_id"`
		OutputIndex  int    `json:"output_index"`
		ContentIndex int    `json:"content_index"`
		Delta        string `json:"delta"` // 增量文本
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	// 发送文本增量给客户端（用于实时字幕）
	g.sendToClient(&ServerMessage{
		Type:     "text_delta",
		Text:     event.Delta,
		ServerTS: time.Now(),
	})

	return nil
}

// handleTextDone 处理文本完成事件
func (g *Gateway) handleTextDone(data []byte) error {
	var event struct {
		Type         string `json:"type"`
		ResponseID   string `json:"response_id"`
		ItemID       string `json:"item_id"`
		OutputIndex  int    `json:"output_index"`
		ContentIndex int    `json:"content_index"`
		Text         string `json:"text"` // 完整文本
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	// 获取当前响应的元数据
	g.activeMetadataLock.RLock()
	metadata := make(map[string]interface{})
	for k, v := range g.activeMetadata {
		metadata[k] = v
	}
	g.activeMetadataLock.RUnlock()

	// 发送完整文本给客户端，附带元数据
	g.sendToClient(&ServerMessage{
		Type:     EventTypeAssistantText,
		Text:     event.Text,
		TurnID:   event.ItemID,
		Metadata: metadata,
		ServerTS: time.Now(),
	})

	// 也转发给Orchestrator（用于记录Timeline）
	asrMsg := &ClientMessage{
		Type:     EventTypeAssistantText,
		Text:     event.Text,
		TurnID:   event.ItemID,
		Metadata: metadata,
		ClientTS: time.Now(),
	}
	return g.forwardToOrchestrator(asrMsg)
}

// handleRealtimeError 处理Realtime错误事件
func (g *Gateway) handleRealtimeError(data []byte) error {
	var event struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	g.logger.Printf("[Gateway] realtime error: type=%s code=%s message=%s",
		event.Error.Type, event.Error.Code, event.Error.Message)

	// 转发错误给客户端
	return g.sendErrorToClient(fmt.Sprintf("Realtime error: %s", event.Error.Message))
}

// SendInstructions 发送导演指令到Realtime（由Orchestrator调用）
// 这是后端"控制Realtime大脑"的关键方法
func (g *Gateway) SendInstructions(_ context.Context, instructions string, metadata map[string]interface{}) error {
	g.logger.Printf("[Gateway] sending instructions to Realtime: %s", instructions)

	// 保存元数据，以便在收到响应时使用
	g.activeMetadataLock.Lock()
	g.activeMetadata = metadata
	g.activeMetadataLock.Unlock()

	// 生成一个 nonce，写入到 response.metadata 里，用于识别 response.created 回传
	nonce := g.nextResponseCreateNonce()
	realtimeMetadata := g.buildRealtimeResponseMetadata(metadata, nonce)
	g.markResponseCreateSent()

	// 构造response.create指令
	create := RealtimeResponseCreate{
		Type: "response.create",
		Response: RealtimeResponseCreateConfig{
			Modalities:   []string{"text", "audio"},
			Instructions: instructions,
			Voice:        g.resolveVoice(metadata),
			Temperature:  0.8,
			Metadata:     realtimeMetadata,
		},
	}

	return g.sendToRealtime(create)
}

func (g *Gateway) markResponseCreateSent() {
	g.lastResponseCreateAtLock.Lock()
	g.lastResponseCreateAt = time.Now()
	g.lastResponseCreateAtLock.Unlock()
}

func (g *Gateway) isLikelyOurResponseByRecentCreate() bool {
	// 经验值：response.created 一般会在 response.create 之后很快返回（毫秒级到秒级）
	const window = 3 * time.Second
	g.lastResponseCreateAtLock.Lock()
	at := g.lastResponseCreateAt
	g.lastResponseCreateAtLock.Unlock()
	if at.IsZero() {
		return false
	}
	return time.Since(at) <= window
}

func (g *Gateway) nextResponseCreateNonce() int64 {
	g.responseCreateNonceLock.Lock()
	defer g.responseCreateNonceLock.Unlock()
	g.responseCreateNonce++
	return g.responseCreateNonce
}

func (g *Gateway) buildRealtimeResponseMetadata(metadata map[string]interface{}, nonce int64) map[string]interface{} {
	// 注意：Realtime metadata 应尽量小，且避免包含敏感信息。
	result := map[string]interface{}{
		"bubbletalk_session_id": g.sessionID,
		// Realtime 对 metadata 值类型有强约束：这里统一用 string，避免模型侧校验失败。
		"bubbletalk_nonce":  fmt.Sprintf("%d", nonce),
		"bubbletalk_source": "orchestrator",
	}
	if metadata == nil {
		return result
	}
	if role, ok := metadata["role"].(string); ok && role != "" {
		result["role"] = role
	}
	if beat, ok := metadata["beat"].(string); ok && beat != "" {
		result["beat"] = beat
	}
	return result
}

func (g *Gateway) isOurRealtimeResponse(realtimeMetadata map[string]interface{}) bool {
	if len(realtimeMetadata) == 0 {
		return false
	}
	if v, ok := realtimeMetadata["bubbletalk_session_id"].(string); !ok || v != g.sessionID {
		return false
	}
	if v, ok := realtimeMetadata["bubbletalk_source"].(string); !ok || v != "orchestrator" {
		return false
	}
	nonce, ok := realtimeMetadata["bubbletalk_nonce"].(string)
	return ok && nonce != ""
}

func (g *Gateway) resolveVoice(metadata map[string]interface{}) string {
	defaultVoice := g.defaultVoice()
	if metadata == nil {
		return defaultVoice
	}
	roleValue, ok := metadata["role"]
	if !ok {
		return defaultVoice
	}
	role, ok := roleValue.(string)
	if !ok || role == "" {
		return defaultVoice
	}
	profile, ok := g.config.RoleProfiles[role]
	if !ok || profile.Voice == "" {
		return defaultVoice
	}
	return profile.Voice
}

func (g *Gateway) defaultVoice() string {
	if g.config.Voice == "" {
		return "alloy"
	}
	return g.config.Voice
}

// sendToRealtime 发送消息到OpenAI Realtime
func (g *Gateway) sendToRealtime(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal realtime message: %w", err)
	}

	g.realtimeConnLock.Lock()
	defer g.realtimeConnLock.Unlock()

	if g.realtimeConn == nil {
		return errors.New("realtime connection is closed")
	}

	if err := g.realtimeConn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write to realtime: %w", err)
	}

	return nil
}

// sendToClient 发送消息给客户端
func (g *Gateway) sendToClient(msg *ServerMessage) error {
	// 分配序列号
	g.seqLock.Lock()
	g.seqCounter++
	msg.Seq = g.seqCounter
	g.seqLock.Unlock()

	// 补充时间戳
	if msg.ServerTS.IsZero() {
		msg.ServerTS = time.Now()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal server message: %w", err)
	}

	g.clientConnLock.Lock()
	defer g.clientConnLock.Unlock()

	if g.clientConn == nil {
		return errors.New("client connection is closed")
	}

	if err := g.clientConn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write to client: %w", err)
	}

	return nil
}

// sendErrorToClient 发送错误消息给客户端
func (g *Gateway) sendErrorToClient(errMsg string) error {
	return g.sendToClient(&ServerMessage{
		Type:     "error",
		Error:    errMsg,
		ServerTS: time.Now(),
	})
}

// pingLoop 定期发送ping保持连接
func (g *Gateway) pingLoop() {
	interval := g.config.PingInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-g.closeChan:
			return
		case <-ticker.C:
			// Ping客户端
			g.clientConnLock.Lock()
			if g.clientConn != nil {
				g.clientConn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
			}
			g.clientConnLock.Unlock()

			// Ping Realtime（可选，OpenAI会自己管理）
			g.realtimeConnLock.Lock()
			if g.realtimeConn != nil {
				g.realtimeConn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
			}
			g.realtimeConnLock.Unlock()
		}
	}
}

// Close 关闭网关
func (g *Gateway) Close() error {
	var closeErr error

	g.closeOnce.Do(func() {
		g.logger.Printf("[Gateway] closing session %s", g.sessionID)

		// 取消context
		g.cancel()

		// 关闭通道
		close(g.closeChan)

		// 关闭连接
		if err := g.closeClientConn(); err != nil {
			closeErr = err
		}
		if err := g.closeRealtimeConn(); err != nil {
			if closeErr == nil {
				closeErr = err
			}
		}
	})

	return closeErr
}

// closeClientConn 关闭客户端连接
func (g *Gateway) closeClientConn() error {
	g.clientConnLock.Lock()
	defer g.clientConnLock.Unlock()

	if g.clientConn == nil {
		return nil
	}

	// 发送关闭消息
	g.clientConn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	)

	err := g.clientConn.Close()
	g.clientConn = nil
	return err
}

// closeRealtimeConn 关闭Realtime连接
func (g *Gateway) closeRealtimeConn() error {
	g.realtimeConnLock.Lock()
	defer g.realtimeConnLock.Unlock()

	if g.realtimeConn == nil {
		return nil
	}

	// 发送关闭消息
	g.realtimeConn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	)

	err := g.realtimeConn.Close()
	g.realtimeConn = nil
	return err
}

// handleFunctionCallArgumentsDelta 处理function call arguments streaming
func (g *Gateway) handleFunctionCallArgumentsDelta(data []byte) error {
	var event struct {
		Type        string `json:"type"`
		ResponseID  string `json:"response_id"`
		ItemID      string `json:"item_id"`
		OutputIndex int    `json:"output_index"`
		CallID      string `json:"call_id"`
		Delta       string `json:"delta"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	g.logger.Printf("[Gateway] 🔧 Function call arguments delta: call_id=%s delta=%s", event.CallID, event.Delta)
	return nil
}

// handleFunctionCallArgumentsDone 处理function call完成并执行工具
func (g *Gateway) handleFunctionCallArgumentsDone(data []byte) error {
	var event struct {
		Type        string `json:"type"`
		ResponseID  string `json:"response_id"`
		ItemID      string `json:"item_id"`
		OutputIndex int    `json:"output_index"`
		CallID      string `json:"call_id"`
		Name        string `json:"name"`
		Arguments   string `json:"arguments"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	g.logger.Printf("[Gateway] 🔧 Function call completed: name=%s call_id=%s args=%s",
		event.Name, event.CallID, event.Arguments)

	// 执行工具
	if g.toolRegistry == nil {
		g.logger.Printf("[Gateway] ⚠️ Tool registry not set, cannot execute function call")
		return nil
	}

	result, err := g.toolRegistry.Execute(g.ctx, event.Name, event.Arguments)
	if err != nil {
		g.logger.Printf("[Gateway] ❌ Tool execution failed: %v", err)
		result = fmt.Sprintf(`{"status":"error","message":"%s"}`, err.Error())
	}

	g.logger.Printf("[Gateway] ✅ Tool executed successfully: %s", result)

	// 发送function_call_output回到Realtime
	if err := g.sendFunctionCallOutput(event.CallID, result); err != nil {
		g.logger.Printf("[Gateway] ❌ Failed to send function call output: %v", err)
		return err
	}

	return nil
}

// sendFunctionCallOutput 发送function call执行结果到Realtime
func (g *Gateway) sendFunctionCallOutput(callID, output string) error {
	// 创建conversation item with function_call_output
	item := map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		},
	}

	if err := g.sendToRealtime(item); err != nil {
		return fmt.Errorf("send function_call_output: %w", err)
	}

	g.logger.Printf("[Gateway] 📤 Sent function_call_output for call_id=%s", callID)
	return nil
}

// SendQuizToClient 发送选择题到客户端
func (g *Gateway) SendQuizToClient(quizID, question string, options []string, context string) error {
	msg := &ServerMessage{
		Type: EventTypeQuizShow,
		QuizData: &QuizMessageData{
			QuizID:   quizID,
			Question: question,
			Options:  options,
			Context:  context,
		},
		ServerTS: time.Now(),
	}

	return g.sendToClient(msg)
}

// Done returns a channel that's closed when the gateway is closed
func (g *Gateway) Done() <-chan struct{} {
	return g.closeChan
}
