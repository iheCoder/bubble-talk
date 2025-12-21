package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MultiVoiceGateway 是支持多音色的语音网关
// 核心架构：
// 1. 每个角色一个独立的 Realtime 连接（voice 固定）
// 2. 一个 ASR 专用连接（只做语音识别）
// 3. 通过"文本镜像"让所有连接共享对话上下文
type MultiVoiceGateway struct {
	sessionID string

	// 客户端连接
	clientConn     *websocket.Conn
	clientConnLock sync.Mutex

	// 音色池（管理多个角色连接）
	voicePool *VoicePool

	// 事件处理器（由 Orchestrator 注入）
	eventHandler EventHandler

	// 状态管理
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// 当前响应的元数据（角色、Beat等）
	activeMetadata     map[string]interface{}
	activeMetadataLock sync.RWMutex

	// 序列号生成器（用于 ServerMessage）
	seqCounter int64
	seqLock    sync.Mutex

	// 配置
	config GatewayConfig

	// 日志
	logger *log.Logger
}

// NewMultiVoiceGateway 创建一个支持多音色的网关
func NewMultiVoiceGateway(sessionID string, clientConn *websocket.Conn, config GatewayConfig) *MultiVoiceGateway {
	ctx, cancel := context.WithCancel(context.Background())

	return &MultiVoiceGateway{
		sessionID:  sessionID,
		clientConn: clientConn,
		ctx:        ctx,
		cancel:     cancel,
		closeChan:  make(chan struct{}),
		config:     config,
		logger:     log.Default(),
	}
}

// SetEventHandler 设置事件处理器（Orchestrator 注入）
func (g *MultiVoiceGateway) SetEventHandler(handler EventHandler) {
	g.eventHandler = handler
}

// Start 启动网关
func (g *MultiVoiceGateway) Start(ctx context.Context) error {
	g.logger.Printf("[MultiVoiceGateway] Starting gateway for session %s", g.sessionID)

	// 1. 创建音色池
	g.logger.Printf("[MultiVoiceGateway] Creating voice pool...")
	roleVoices := make(map[string]string)
	for role, profile := range g.config.RoleProfiles {
		roleVoices[role] = profile.Voice
		g.logger.Printf("[MultiVoiceGateway] Role %s -> Voice %s", role, profile.Voice)
	}

	poolConfig := VoicePoolConfig{
		OpenAIAPIKey:                 g.config.OpenAIAPIKey,
		Model:                        g.config.Model,
		DefaultInstructions:          g.config.DefaultInstructions,
		InputAudioFormat:             g.config.InputAudioFormat,
		OutputAudioFormat:            g.config.OutputAudioFormat,
		InputAudioTranscriptionModel: g.config.InputAudioTranscriptionModel,
		RoleVoices:                   roleVoices,
	}

	g.voicePool = NewVoicePool(g.sessionID, poolConfig)

	// 2. 初始化音色池（创建所有 RoleConn 和 ASRConn）
	g.logger.Printf("[MultiVoiceGateway] Initializing voice pool...")
	if err := g.voicePool.Initialize(ctx); err != nil {
		g.logger.Printf("[MultiVoiceGateway] ❌ Failed to initialize voice pool: %v", err)
		return fmt.Errorf("initialize voice pool: %w", err)
	}
	g.logger.Printf("[MultiVoiceGateway] ✅ Voice pool initialized")

	// 3. 启动事件循环
	g.logger.Printf("[MultiVoiceGateway] Starting event loops...")
	go g.clientReadLoop()
	go g.asrReadLoop()
	go g.roleConnsReadLoop()

	g.logger.Printf("[MultiVoiceGateway] ✅ Gateway fully started for session %s", g.sessionID)
	return nil
}

// clientReadLoop 从客户端读取消息（事件+音频）
func (g *MultiVoiceGateway) clientReadLoop() {
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
				g.logger.Printf("[MultiVoiceGateway] client read error: %v", err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			// JSON 事件
			if err := g.handleClientEvent(data); err != nil {
				g.logger.Printf("[MultiVoiceGateway] handle client event error: %v", err)
				g.sendErrorToClient(err.Error())
			}
		} else if messageType == websocket.BinaryMessage {
			// 音频数据（发送到 ASR 连接）
			if err := g.handleClientAudio(data); err != nil {
				g.logger.Printf("[MultiVoiceGateway] handle client audio error: %v", err)
			}
		}
	}
}

// handleClientEvent 处理客户端 JSON 事件
func (g *MultiVoiceGateway) handleClientEvent(data []byte) error {
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal client message: %w", err)
	}

	if msg.ClientTS.IsZero() {
		msg.ClientTS = time.Now()
	}

	g.logger.Printf("[MultiVoiceGateway] client event: type=%s event_id=%s", msg.Type, msg.EventID)

	switch msg.Type {
	case EventTypeBargeIn:
		return g.handleBargeIn(&msg)
	case EventTypeExitRequested, EventTypeQuizAnswer:
		return g.forwardToOrchestrator(&msg)
	default:
		return g.forwardToOrchestrator(&msg)
	}
}

// handleClientAudio 处理客户端音频数据（发送到 ASR 连接）
func (g *MultiVoiceGateway) handleClientAudio(audioData []byte) error {
	// 将音频数据转发到 ASR 连接
	asrConn, err := g.voicePool.GetASRConn()
	if err != nil {
		return fmt.Errorf("get ASR conn: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(audioData)
	append := RealtimeInputAudioBufferAppend{
		Type:  "input_audio_buffer.append",
		Audio: encoded,
	}

	return asrConn.SendMessage(append)
}

// handleBargeIn 处理插话中断
func (g *MultiVoiceGateway) handleBargeIn(msg *ClientMessage) error {
	g.logger.Printf("[MultiVoiceGateway] barge-in detected, canceling active response")

	// 取消当前正在说话的角色的响应
	if err := g.voicePool.CancelCurrentResponse(); err != nil {
		g.logger.Printf("[MultiVoiceGateway] failed to cancel response: %v", err)
	}

	// 通知客户端清空音频缓冲区
	g.sendToClient(&ServerMessage{
		Type:     EventTypeTTSInterrupted,
		ServerTS: time.Now(),
	})

	// 转发给 Orchestrator
	return g.forwardToOrchestrator(msg)
}

// forwardToOrchestrator 转发事件给 Orchestrator
func (g *MultiVoiceGateway) forwardToOrchestrator(msg *ClientMessage) error {
	if g.eventHandler == nil {
		g.logger.Printf("[MultiVoiceGateway] no event handler set, dropping event: %s", msg.Type)
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
		defer cancel()

		if err := g.eventHandler(ctx, msg); err != nil {
			g.logger.Printf("[MultiVoiceGateway] orchestrator handler error: %v", err)
		}
	}()

	return nil
}

// asrReadLoop 从 ASR 连接读取消息
func (g *MultiVoiceGateway) asrReadLoop() {
	asrConn, err := g.voicePool.GetASRConn()
	if err != nil {
		g.logger.Printf("[MultiVoiceGateway] ❌ Failed to get ASR conn: %v", err)
		return
	}

	for {
		select {
		case <-g.closeChan:
			return
		default:
		}

		messageType, data, err := asrConn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				g.logger.Printf("[MultiVoiceGateway] ASR read error: %v", err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			if err := g.handleASREvent(data); err != nil {
				g.logger.Printf("[MultiVoiceGateway] handle ASR event error: %v", err)
			}
		}
	}
}

// handleASREvent 处理 ASR 连接的事件
func (g *MultiVoiceGateway) handleASREvent(data []byte) error {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshal ASR event: %w", err)
	}

	eventType, _ := event["type"].(string)
	g.logger.Printf("[MultiVoiceGateway] ASR event: %s", eventType)

	switch eventType {
	case "conversation.item.input_audio_transcription.completed":
		// 用户语音转写完成
		return g.handleASRTranscriptionCompleted(event)

	case "input_audio_buffer.speech_started":
		// 用户开始说话
		g.logger.Printf("[MultiVoiceGateway] User started speaking")
		// 可以选择在这里触发插话中断
		// 但通常我们让客户端发送 barge_in 事件更准确

	case "input_audio_buffer.speech_stopped":
		// 用户停止说话
		g.logger.Printf("[MultiVoiceGateway] User stopped speaking")
	}

	return nil
}

// handleASRTranscriptionCompleted 处理转写完成事件
func (g *MultiVoiceGateway) handleASRTranscriptionCompleted(event map[string]interface{}) error {
	transcript, _ := event["transcript"].(string)
	if transcript == "" {
		g.logger.Printf("[MultiVoiceGateway] Empty transcript, ignoring")
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] User transcript: %s", transcript)

	// 1. 同步用户文本到所有角色连接（文本镜像）
	if err := g.voicePool.SyncUserText(transcript); err != nil {
		g.logger.Printf("[MultiVoiceGateway] ⚠️  Failed to sync user text: %v", err)
	}

	// 2. 转发给 Orchestrator 处理
	msg := &ClientMessage{
		Type:     EventTypeASRFinal,
		EventID:  fmt.Sprintf("asr_%d", time.Now().UnixNano()),
		Text:     transcript,
		ClientTS: time.Now(),
	}

	return g.forwardToOrchestrator(msg)
}

// roleConnsReadLoop 从所有角色连接读取消息
func (g *MultiVoiceGateway) roleConnsReadLoop() {
	// 为每个角色连接启动一个读取协程
	for role := range g.config.RoleProfiles {
		role := role // 捕获循环变量
		go g.roleConnReadLoop(role)
	}
}

// roleConnReadLoop 从指定角色连接读取消息
func (g *MultiVoiceGateway) roleConnReadLoop(role string) {
	conn, err := g.voicePool.GetRoleConn(role)
	if err != nil {
		g.logger.Printf("[MultiVoiceGateway] ❌ Failed to get role conn for %s: %v", role, err)
		return
	}

	for {
		select {
		case <-g.closeChan:
			return
		default:
		}

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				g.logger.Printf("[MultiVoiceGateway] Role %s read error: %v", role, err)
			}
			return
		}

		if messageType == websocket.TextMessage {
			if err := g.handleRoleConnEvent(role, data); err != nil {
				g.logger.Printf("[MultiVoiceGateway] handle role conn event error: %v", err)
			}
		}
	}
}

// handleRoleConnEvent 处理角色连接的事件
func (g *MultiVoiceGateway) handleRoleConnEvent(role string, data []byte) error {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshal role conn event: %w", err)
	}

	eventType, _ := event["type"].(string)
	g.logger.Printf("[MultiVoiceGateway] Role %s event: %s", role, eventType)

	switch eventType {
	case "response.created":
		// 响应创建
		responseID, _ := event["response"].(map[string]interface{})["id"].(string)
		conn, _ := g.voicePool.GetRoleConn(role)
		if conn != nil {
			conn.SetActiveResponse(responseID)
		}

	case "response.audio.delta":
		// 音频增量（转发给客户端）
		return g.handleAudioDelta(role, event)

	case "response.audio_transcript.delta":
		// 文本增量（可选：显示实时字幕）
		delta, _ := event["delta"].(string)
		g.logger.Printf("[MultiVoiceGateway] Role %s transcript delta: %s", role, delta)

	case "response.done":
		// 响应完成
		return g.handleResponseDone(role, event)
	}

	return nil
}

// handleAudioDelta 处理音频增量
func (g *MultiVoiceGateway) handleAudioDelta(role string, event map[string]interface{}) error {
	delta, _ := event["delta"].(string)
	if delta == "" {
		return nil
	}

	// 解码 Base64
	audioData, err := base64.StdEncoding.DecodeString(delta)
	if err != nil {
		return fmt.Errorf("decode audio delta: %w", err)
	}

	// 转发给客户端（作为二进制消息）
	g.clientConnLock.Lock()
	defer g.clientConnLock.Unlock()

	if err := g.clientConn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
		return fmt.Errorf("write audio to client: %w", err)
	}

	return nil
}

// handleResponseDone 处理响应完成事件
func (g *MultiVoiceGateway) handleResponseDone(role string, event map[string]interface{}) error {
	g.logger.Printf("[MultiVoiceGateway] Role %s response done", role)

	// 清除活跃响应
	conn, _ := g.voicePool.GetRoleConn(role)
	if conn != nil {
		conn.ClearActiveResponse()
	}

	// 清除正在说话的角色
	g.voicePool.ClearSpeakingRole()

	// 提取最终文本
	response, _ := event["response"].(map[string]interface{})
	output, _ := response["output"].([]interface{})

	var finalText string
	for _, item := range output {
		itemMap, _ := item.(map[string]interface{})
		itemType, _ := itemMap["type"].(string)
		if itemType == "message" {
			content, _ := itemMap["content"].([]interface{})
			for _, c := range content {
				cMap, _ := c.(map[string]interface{})
				if cMap["type"] == "text" {
					text, _ := cMap["text"].(string)
					finalText += text
				} else if cMap["type"] == "audio" {
					transcript, _ := cMap["transcript"].(string)
					finalText += transcript
				}
			}
		}
	}

	if finalText != "" {
		g.logger.Printf("[MultiVoiceGateway] Role %s final text: %s", role, finalText)

		// 同步到所有其他角色连接（文本镜像）
		if err := g.voicePool.SyncAssistantText(finalText, role); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ⚠️  Failed to sync assistant text: %v", err)
		}
	}

	return nil
}

// SendInstructions 发送指令到指定角色的连接
func (g *MultiVoiceGateway) SendInstructions(ctx context.Context, instructions string, metadata map[string]interface{}) error {
	// 从 metadata 中提取角色
	role, ok := metadata["role"].(string)
	if !ok || role == "" {
		return fmt.Errorf("role not specified in metadata")
	}

	g.logger.Printf("[MultiVoiceGateway] Sending instructions to role %s (len=%d)", role, len(instructions))

	// 保存活跃元数据
	g.activeMetadataLock.Lock()
	g.activeMetadata = metadata
	g.activeMetadataLock.Unlock()

	// 在指定角色的连接上创建响应
	return g.voicePool.CreateResponse(role, instructions, metadata)
}

// sendToClient 发送消息给客户端
func (g *MultiVoiceGateway) sendToClient(msg *ServerMessage) error {
	g.seqLock.Lock()
	g.seqCounter++
	msg.Seq = g.seqCounter
	g.seqLock.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal server message: %w", err)
	}

	g.clientConnLock.Lock()
	defer g.clientConnLock.Unlock()

	return g.clientConn.WriteMessage(websocket.TextMessage, data)
}

// sendErrorToClient 发送错误消息给客户端
func (g *MultiVoiceGateway) sendErrorToClient(errMsg string) {
	_ = g.sendToClient(&ServerMessage{
		Type:     "error",
		Error:    errMsg,
		ServerTS: time.Now(),
	})
}

// Close 关闭网关
func (g *MultiVoiceGateway) Close() error {
	g.logger.Printf("[MultiVoiceGateway] Closing gateway for session %s", g.sessionID)

	g.closeOnce.Do(func() {
		g.cancel()
		close(g.closeChan)

		// 关闭音色池
		if g.voicePool != nil {
			_ = g.voicePool.Close()
		}

		// 关闭客户端连接
		g.clientConnLock.Lock()
		if g.clientConn != nil {
			_ = g.clientConn.Close()
		}
		g.clientConnLock.Unlock()
	})

	return nil
}

// Done 返回一个在连接关闭时关闭的 channel
func (g *MultiVoiceGateway) Done() <-chan struct{} {
	return g.closeChan
}
