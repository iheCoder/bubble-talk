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

	// 状态管理
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// 当前活跃的响应ID（用于barge-in取消）
	activeResponseID     string
	activeResponseIDLock sync.RWMutex

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
	OpenAIRealtimeURL string // wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-12-17
	Model             string
	Voice             string

	// 默认指令（基础人设）
	DefaultInstructions string

	// 超时配置
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration

	// 音频配置
	InputAudioFormat  string // pcm16
	OutputAudioFormat string // pcm16
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
	// 1. 连接OpenAI Realtime
	if err := g.connectRealtime(ctx); err != nil {
		return fmt.Errorf("connect realtime: %w", err)
	}

	// 2. 初始化会话配置
	if err := g.initRealtimeSession(ctx); err != nil {
		g.closeRealtimeConn()
		return fmt.Errorf("init realtime session: %w", err)
	}

	// 3. 启动事件循环
	go g.clientReadLoop()
	go g.realtimeReadLoop()
	go g.pingLoop()

	g.logger.Printf("[Gateway] started for session %s", g.sessionID)
	return nil
}

// connectRealtime 连接到OpenAI Realtime API
func (g *Gateway) connectRealtime(ctx context.Context) error {
	url := g.config.OpenAIRealtimeURL
	if url == "" {
		model := g.config.Model
		if model == "" {
			model = "gpt-4o-realtime-preview-2024-12-17"
		}
		url = fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", model)
	}

	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Bearer " + g.config.OpenAIAPIKey}
	headers["OpenAI-Beta"] = []string{"realtime=v1"}

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial realtime: status=%d err=%w", resp.StatusCode, err)
		}
		return fmt.Errorf("dial realtime: %w", err)
	}

	g.realtimeConn = conn
	g.logger.Printf("[Gateway] connected to OpenAI Realtime: %s", url)
	return nil
}

// initRealtimeSession 初始化Realtime会话配置
func (g *Gateway) initRealtimeSession(ctx context.Context) error {
	// 构造session.update指令
	update := RealtimeSessionUpdate{
		Type: "session.update",
		Session: RealtimeSessionConfig{
			Modalities:        []string{"text", "audio"},
			Instructions:      g.config.DefaultInstructions,
			Voice:             g.config.Voice,
			InputAudioFormat:  g.config.InputAudioFormat,
			OutputAudioFormat: g.config.OutputAudioFormat,
			TurnDetection: &TurnDetectionConfig{
				Type:              "server_vad",
				Threshold:         0.5,
				PrefixPaddingMS:   300,
				SilenceDurationMS: 500, // 500ms静音认为说完
			},
			Temperature: 0.8,
		},
	}

	if g.config.Voice == "" {
		update.Session.Voice = "alloy"
	}
	if g.config.InputAudioFormat == "" {
		update.Session.InputAudioFormat = "pcm16"
	}
	if g.config.OutputAudioFormat == "" {
		update.Session.OutputAudioFormat = "pcm16"
	}

	return g.sendToRealtime(update)
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

	case "response.created":
		// 响应创建
		return g.handleResponseCreated(data)

	case "response.output_item.added":
		// 输出项添加
		return g.handleResponseOutputItemAdded(data)

	case "response.content_part.added":
		// 内容部分添加
		return nil

	case "response.audio.delta":
		// TTS音频流（转发给客户端）
		return g.handleAudioDelta(data)

	case "response.audio.done":
		// TTS完成
		return g.handleAudioDone(data)

	case "response.done":
		// 响应完成
		return g.handleResponseDone(data)

	case "response.text.delta":
		// 文本流（可选，用于字幕）
		return g.handleTextDelta(data)

	case "response.text.done":
		// 文本完成
		return g.handleTextDone(data)

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
func (g *Gateway) handleSpeechStopped(data []byte) error {
	// 通知客户端
	g.sendToClient(&ServerMessage{
		Type:     "speech_stopped",
		ServerTS: time.Now(),
	})
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

	// 如果是用户消息且有转写文本，提取ASR结果
	if event.Item.Role == "user" && len(event.Item.Content) > 0 {
		for _, content := range event.Item.Content {
			if content.Transcript != "" {
				// 这是ASR最终转写，发送给Orchestrator
				asrMsg := &ClientMessage{
					Type:     EventTypeASRFinal,
					Text:     content.Transcript,
					TurnID:   event.Item.ID,
					ClientTS: time.Now(),
				}

				// 转发给Orchestrator
				if err := g.forwardToOrchestrator(asrMsg); err != nil {
					return err
				}

				// 也发送给客户端（用于UI显示）
				g.sendToClient(&ServerMessage{
					Type:     EventTypeASRFinal,
					Text:     content.Transcript,
					TurnID:   event.Item.ID,
					ServerTS: time.Now(),
				})
			}
		}
	}

	return nil
}

// handleResponseCreated 处理响应创建事件
func (g *Gateway) handleResponseCreated(data []byte) error {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}

	// 记录活跃响应ID（用于barge-in取消）
	g.activeResponseIDLock.Lock()
	g.activeResponseID = event.Response.ID
	g.activeResponseIDLock.Unlock()

	return nil
}

// handleResponseOutputItemAdded 处理输出项添加事件
func (g *Gateway) handleResponseOutputItemAdded(data []byte) error {
	// TTS开始
	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSStarted,
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
	// 通知客户端TTS完成
	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSCompleted,
		ServerTS: time.Now(),
	})
	return nil
}

// handleResponseDone 处理响应完成事件
func (g *Gateway) handleResponseDone(data []byte) error {
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

	// 发送完整文本给客户端
	g.sendToClient(&ServerMessage{
		Type:     EventTypeAssistantText,
		Text:     event.Text,
		TurnID:   event.ItemID,
		ServerTS: time.Now(),
	})

	// 也转发给Orchestrator（用于记录Timeline）
	asrMsg := &ClientMessage{
		Type:     EventTypeAssistantText,
		Text:     event.Text,
		TurnID:   event.ItemID,
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
func (g *Gateway) SendInstructions(ctx context.Context, instructions string, metadata map[string]interface{}) error {
	g.logger.Printf("[Gateway] sending instructions to Realtime: %s", instructions)

	// 构造response.create指令
	create := RealtimeResponseCreate{
		Type: "response.create",
		Response: RealtimeResponseCreateConfig{
			Modalities:   []string{"text", "audio"},
			Instructions: instructions,
			Voice:        g.config.Voice,
			Temperature:  0.8,
		},
	}

	if g.config.Voice == "" {
		create.Response.Voice = "alloy"
	}

	return g.sendToRealtime(create)
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
