package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"bubble-talk/server/internal/tool"

	"github.com/gorilla/websocket"
)

// MultiVoiceGateway 是支持多音色的语音网关
// 核心架构：
//  1. 每个角色一个独立的 Realtime 连接（voice 固定）：负责该角色的语音合成（TTS）和对话逻辑。
//  2. 一个 ASR 专用连接（只做语音识别）：负责接收用户的音频流，进行语音转文字（STT）。
//     这个连接通常配置为不生成音频，或者生成的音频被丢弃。
//  3. 通过"文本镜像"让所有连接共享对话上下文：
//     - 当 ASR 识别到用户说话时，将文本作为 Text Item 注入到所有角色连接。
//     - 当某个角色说话时，将其回复文本作为 Assistant Item 注入到其他角色连接。
//     这样所有连接都能维护完整的对话历史。
type MultiVoiceGateway struct {
	sessionID string

	// 客户端连接：与前端（Web/App）的 WebSocket 连接，传输音频流和控制指令
	clientConn     *websocket.Conn
	clientConnLock sync.Mutex

	// 音色池（管理多个角色连接）：封装了与 OpenAI Realtime API 的多个连接
	voicePool *VoicePool

	// 事件处理器（由 Orchestrator 注入）：用于将网关收到的业务事件（如用户说话、插话、退出等）转发给编排器
	eventHandler EventHandler

	// 事件队列：串行处理事件，防止并发修改 SessionState
	eventQueue *EventQueue

	// 工具注册表（支持function calling）：所有角色共享的工具集
	toolRegistry *tool.ToolRegistry

	// 响应元数据注册表（解决音频与元数据关联问题）
	metadataRegistry *ResponseMetadataRegistry

	// 状态管理
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// ASR 事件源配置：解决"双终态事件源"问题
	// true: 只使用 conversation.item.input_audio_transcription.completed
	// false: 只使用 response.done/response.audio_transcript.done
	useTranscriptionCompleted bool

	// 当前响应的元数据（角色、Beat等）：记录当前正在说话的角色和对应的剧情节拍信息
	activeMetadata     map[string]interface{}
	activeMetadataLock sync.RWMutex

	// ASR 去重（避免 response.done 与 response.audio_transcript.done 重复触发）
	// OpenAI Realtime API 可能会通过多种事件返回转写结果，我们需要去重以避免重复处理
	asrDedupMu          sync.Mutex
	lastASRResponseID   string
	lastASRTranscript   string
	lastASRTranscriptAt time.Time

	// 序列号生成器（用于 ServerMessage）：保证发送给客户端的消息有序
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

	logger := log.Default()
	g := &MultiVoiceGateway{
		sessionID:  sessionID,
		clientConn: clientConn,
		ctx:        ctx,
		cancel:     cancel,
		closeChan:  make(chan struct{}),
		config:     config,
		logger:     logger,
		// 默认使用 transcription.completed 作为唯一 ASR 事件源
		useTranscriptionCompleted: true,
		// 初始化元数据注册表
		metadataRegistry: NewResponseMetadataRegistry(logger),
	}

	return g
}

// SetEventHandler 设置事件处理器（Orchestrator 注入）
func (g *MultiVoiceGateway) SetEventHandler(handler EventHandler) {
	g.eventHandler = handler
	// 创建事件队列，确保事件串行处理
	if g.eventQueue == nil {
		g.eventQueue = NewEventQueue(g.sessionID, handler, g.logger)
		g.logger.Printf("[MultiVoiceGateway] Event queue created for session %s", g.sessionID)
	}
}

// SetToolRegistry 设置工具注册表
func (g *MultiVoiceGateway) SetToolRegistry(registry *tool.ToolRegistry) {
	g.toolRegistry = registry
	// 如果音色池已经初始化，传递给所有角色连接
	if g.voicePool != nil {
		g.voicePool.SetToolRegistry(registry)
	}
}

// Start 启动网关
func (g *MultiVoiceGateway) Start(ctx context.Context) error {
	g.logger.Printf("[MultiVoiceGateway] Starting gateway for session %s", g.sessionID)

	if g.clientConn == nil {
		return fmt.Errorf("clientConn is nil")
	}

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

	// 传递工具注册表到音色池（如果已设置）
	if g.toolRegistry != nil {
		g.voicePool.SetToolRegistry(g.toolRegistry)
		g.logger.Printf("[MultiVoiceGateway] Tool registry passed to voice pool")
	}

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
// 修复方案：使用事件队列串行处理，防止 SessionState 并发修改
func (g *MultiVoiceGateway) forwardToOrchestrator(msg *ClientMessage) error {
	if g.eventHandler == nil {
		g.logger.Printf("[MultiVoiceGateway] ⚠️  no event handler set, dropping event: %s", msg.Type)
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] Forwarding event to Orchestrator: type=%s text=%s", msg.Type, msg.Text)

	// 使用事件队列代替直接的 goroutine，保证：
	// 1. 同一 session 的所有事件串行处理（防止并发写 SessionState）
	// 2. 事件按接收顺序处理（防止 asr_final 和 assistant_text 乱序）
	if g.eventQueue != nil {
		if err := g.eventQueue.Enqueue(msg); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ❌ Failed to enqueue event: %v", err)
			g.sendErrorToClient(fmt.Sprintf("Event queue error: %v", err))
			return err
		}
		return nil
	}

	// 降级方案：如果事件队列未初始化，使用同步处理（不再使用 goroutine）
	// 这样虽然可能阻塞，但至少不会出现并发问题
	g.logger.Printf("[MultiVoiceGateway] ⚠️  Event queue not initialized, using synchronous processing")
	ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
	defer cancel()

	if err := g.eventHandler(ctx, msg); err != nil {
		g.logger.Printf("[MultiVoiceGateway] ❌ Orchestrator handler error: %v", err)
		g.sendErrorToClient(fmt.Sprintf("Orchestrator error: %v", err))
		return err
	}

	g.logger.Printf("[MultiVoiceGateway] ✅ Orchestrator handled event successfully")
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
// ASR 连接的主要职责是接收用户音频并转写为文本，它不应该生成音频响应。
// 但由于 OpenAI Realtime API 的机制，VAD 触发时可能会自动创建 response。
// 我们需要处理这些事件，提取转写文本，并确保不会产生不需要的音频输出。
func (g *MultiVoiceGateway) handleASREvent(data []byte) error {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshal ASR event: %w", err)
	}

	eventType, _ := event["type"].(string)
	g.logger.Printf("[MultiVoiceGateway] ASR event: %s", eventType)

	switch eventType {
	case "error":
		// 记录 API 错误
		g.logRealtimeError("ASR", event)
		return nil

	// ASR 相关事件
	case "input_audio_buffer.speech_started":
		// VAD 检测到用户开始说话
		// 修复方案：服务端兜底的插话检测
		g.logger.Printf("[MultiVoiceGateway] 🎤 User started speaking (server-side VAD)")

		// 服务端兜底：如果有角色正在说话，立即取消
		// 这是对客户端 barge_in 的补充，防止客户端延迟或未发送 barge_in
		if err := g.voicePool.CancelCurrentResponse(); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ⚠️  Server-side barge-in cancel failed: %v", err)
		} else {
			g.logger.Printf("[MultiVoiceGateway] ✅ Server-side barge-in: cancelled current response")
		}

		return nil

	case "input_audio_buffer.speech_stopped":
		// VAD 检测到用户停止说话
		// 注意不等同于用户真的说完了，可能只是短暂停顿、VAD 静音阈值触发
		g.logger.Printf("[MultiVoiceGateway] User stopped speaking")
		return nil

	case "conversation.item.input_audio_transcription.completed":
		// ASR 已完成，服务器生成了"最终可用的用户语音文本"，用户"说了什么"在这一刻才确定
		// 当 session 配置了 input_audio_transcription 时触发
		// 修复方案：这是 ASR 的唯一真相来源（如果 useTranscriptionCompleted=true）
		if g.useTranscriptionCompleted {
			return g.handleASRTranscriptionCompleted(event)
		}
		g.logger.Printf("[MultiVoiceGateway] Ignoring transcription.completed (using response.done as ASR source)")
		return nil

	// response 生命周期事件
	case "response.created":
		// 一个新的 response 生命周期被创建了
		// ASR 连接不应该创建 response，但由于 API 默认行为（VAD 触发 response），它可能会创建。
		// 我们需要记录这个 response ID，以便后续提取转写或取消它。
		// 关键点：ASR 连接的 instructions 通常设置为空或"只做转写"，以减少模型生成内容的消耗。
		responseID, _ := event["response"].(map[string]interface{})["id"].(string)
		if responseID != "" {
			g.logger.Printf("[MultiVoiceGateway] ⚠️ ASR created response %s (will extract transcription and cancel)", responseID)
			asrConn, _ := g.voicePool.GetASRConn()
			if asrConn != nil {
				asrConn.SetActiveResponse(responseID)
			}
		}

	case "response.audio_transcript.delta":
		// assistant 语音输出对应的“转写文本（实时）”的增量
		// 忽略 ASR 的音频转写增量（我们只关心最终文本，避免频繁刷新 UI 或逻辑）
		return nil

	case "response.audio_transcript.done", "response.done":
		// assistant 语音转写文本已完成，response 生命周期彻底结束
		// 修复方案：只在 useTranscriptionCompleted=false 时使用这个作为 ASR 源
		if !g.useTranscriptionCompleted {
			return g.handleASRResponseDone(event)
		}

		// 如果使用 transcription.completed，这里仍然要取消 ASR response，但不上报文本
		g.logger.Printf("[MultiVoiceGateway] ASR response done (will cancel but not report, using transcription.completed as source)")
		asrConn, _ := g.voicePool.GetASRConn()
		if asrConn != nil {
			if err := asrConn.CancelResponse(); err != nil {
				g.logger.Printf("[MultiVoiceGateway] ⚠️ Failed to cancel ASR response: %v", err)
			}
		}
		return nil

	}

	return nil
}

// handleASRResponseDone 从 ASR response 中提取转写并取消 response
// 目的：获取用户输入的文本内容，同时确保 ASR 连接不播放音频。
func (g *MultiVoiceGateway) handleASRResponseDone(event map[string]interface{}) error {
	// 从 response 中提取转写
	// 结构可能是 response.output[].content[].transcript (response.done)
	// 或者是直接的 transcript 字段 (response.audio_transcript.done)
	var transcript string
	responseID := ""

	if event["type"] == "response.done" {
		response, _ := event["response"].(map[string]interface{})
		if response != nil {
			responseID, _ = response["id"].(string)
		}
		output, _ := response["output"].([]interface{})

		for _, item := range output {
			itemMap, _ := item.(map[string]interface{})
			itemType, _ := itemMap["type"].(string)
			if itemType == "message" {
				content, _ := itemMap["content"].([]interface{})
				for _, c := range content {
					cMap, _ := c.(map[string]interface{})
					if cMap["type"] == "audio" {
						text, _ := cMap["transcript"].(string)
						transcript += text
					}
				}
			}
		}
	} else {
		// response.audio_transcript.done
		if v, ok := event["response_id"].(string); ok {
			responseID = v
		}
		transcript, _ = event["transcript"].(string)
	}

	if transcript == "" {
		g.logger.Printf("[MultiVoiceGateway] ⚠️ Empty ASR transcript")
		return nil
	}

	// ASR 去重：避免同一个 response 的多次事件导致重复处理
	if g.shouldDropASRResult(responseID, transcript) {
		g.logger.Printf("[MultiVoiceGateway] ⚠️ Duplicate ASR transcript dropped (response_id=%s)", responseID)
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] 📝 ASR transcription: %s", transcript)

	// 取消 ASR response（我们不需要它的音频输出，防止它"说话"）
	// 虽然 response 已经 done，但取消操作可以确保清理相关状态
	asrConn, _ := g.voicePool.GetASRConn()
	if asrConn != nil {
		if err := asrConn.CancelResponse(); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ⚠️ Failed to cancel ASR response: %v", err)
		}
	}

	// 1. 同步用户文本到所有角色连接（文本镜像）
	// 这是多音色架构的关键：让所有角色都知道用户说了什么，
	// 即使它们不是接收音频的那个连接。
	if err := g.voicePool.SyncUserText(transcript); err != nil {
		g.logger.Printf("[MultiVoiceGateway] ⚠️  Failed to sync user text: %v", err)
	}

	// 2. 转发给 Orchestrator 处理
	// Orchestrator 会根据这个文本决定下一步的剧情（Beat）或让哪个角色回答。
	msg := &ClientMessage{
		Type:     EventTypeASRFinal,
		EventID:  fmt.Sprintf("asr_%d", time.Now().UnixNano()),
		Text:     transcript,
		ClientTS: time.Now(),
	}

	return g.forwardToOrchestrator(msg)
}

// handleASRTranscriptionCompleted 处理转写完成事件
// 修复方案：这是 ASR 的唯一真相来源（当 useTranscriptionCompleted=true）
// 避免与 handleASRResponseDone 重复上报同一句话
func (g *MultiVoiceGateway) handleASRTranscriptionCompleted(event map[string]interface{}) error {
	transcript, _ := event["transcript"].(string)
	if transcript == "" {
		g.logger.Printf("[MultiVoiceGateway] Empty transcript, ignoring")
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] ✅ [PRIMARY ASR SOURCE] User transcript: %s", transcript)

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
	conn, err := g.voicePool.GetRoleConn(g.ctx, role)
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
// 角色连接主要负责 TTS（语音合成）和对话逻辑。
// 我们需要监听这些事件来同步状态、转发音频给客户端，以及进行文本镜像。
func (g *MultiVoiceGateway) handleRoleConnEvent(role string, data []byte) error {
	var event map[string]interface{}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("unmarshal role conn event: %w", err)
	}

	eventType, _ := event["type"].(string)
	g.logger.Printf("[MultiVoiceGateway] Role %s event: %s", role, eventType)

	switch eventType {
	case "error":
		g.logRealtimeError("Role "+role, event)
		return nil

	case "response.created":
		// 响应创建 - 意味着角色准备开始说话
		// 1. 记录 active response ID，以便在插话时能取消它
		responseID, _ := event["response"].(map[string]interface{})["id"].(string)
		conn, _ := g.voicePool.GetRoleConn(g.ctx, role)
		if conn != nil {
			conn.SetActiveResponse(responseID)

			// 2. 注册元数据：将 responseID 与 role、metadata 关联
			if metadata := conn.GetPendingMetadata(); metadata != nil {
				g.metadataRegistry.Register(responseID, role, metadata)
				g.logger.Printf("[MultiVoiceGateway] ✅ Registered metadata for responseID=%s role=%s",
					responseID, role)
			} else {
				g.logger.Printf("[MultiVoiceGateway] ⚠️  No pending metadata for responseID=%s role=%s",
					responseID, role)
			}
		}

		// 3. 发送 tts_started 给前端，包含角色信息，让前端显示"正在说话"的动画
		g.sendTTSStartedToClient(role)

	case "response.audio.delta":
		// 音频增量 - 这是实时的语音数据
		// 我们直接转发给客户端播放
		return g.handleAudioDelta(role, event)

	case "response.audio_transcript.delta":
		// 文本增量 - 实时字幕
		// 目前只打印日志，如果前端需要实时逐字显示，可以转发这个事件
		delta, _ := event["delta"].(string)
		g.logger.Printf("[MultiVoiceGateway] Role %s transcript delta: %s", role, delta)

	case "response.done":
		// 响应完成 - 角色说完了一句话
		// 1. 通知前端 TTS 结束
		g.sendTTSCompletedToClient(role)
		// 2. 提取完整文本，进行文本镜像（同步给其他角色）和业务处理
		return g.handleResponseDone(role, event)

	case "response.cancelled":
		// 响应被取消 - 插话中断成功
		// 修复方案：处理取消后的状态收敛
		g.logger.Printf("[MultiVoiceGateway] ✅ Role %s response cancelled (barge-in successful)", role)

		// 提取 responseID
		response, _ := event["response"].(map[string]interface{})
		responseID, _ := response["id"].(string)

		// 1. 清除活跃响应
		conn, _ := g.voicePool.GetRoleConn(g.ctx, role)
		if conn != nil {
			conn.ClearActiveResponse()
		}

		// 2. 清除正在说话的角色
		g.voicePool.ClearSpeakingRole()

		// 3. 注销元数据
		if responseID != "" {
			g.metadataRegistry.Unregister(responseID)
			g.logger.Printf("[MultiVoiceGateway] ✅ Unregistered metadata for cancelled responseID=%s", responseID)
		}

		// 4. 通知前端 TTS 已中断
		g.sendTTSCompletedToClient(role)

		return nil
	}

	return nil
}

const asrDuplicateWindow = 2 * time.Second

// shouldDropASRResult 用于去重 ASR 的重复完成事件。
//
// 说明（中文）：
// 这个函数用于避免 ASR 连接重复触发同一条最终转写（completion）导致的重复上报。
// ASR 在某些情况下会对同一段语音既产生 response.done（完整 response 包含 output）
// 又产生 response.audio_transcript.done（单独的转写完成事件），或者客户端/服务端在短时间内
// 收到同样的转写两次。因此需要简单的去重策略来避免上层（比如 Orchestrator）重复处理相同的文本。
//
// 去重策略：
// 1) 优先基于 responseID 去重：
//   - 如果本次事件包含非空的 responseID，且与上一次记录的 lastASRResponseID 相同，
//     则认为是同一次 response 的重复完成事件，直接返回 true（丢弃）。
//   - 否则将 lastASRResponseID 更新为当前 responseID，同时更新 lastASRTranscript/lastASRTranscriptAt
//     为当前 transcript 与时间，并返回 false（不丢弃）。
//     说明：responseID 是最可靠的去重键，因为同一个 response 的不同完成事件（例如 audio_transcript.done
//     与 response.done）通常会带相同的 response id。
//
// 2) 当 responseID 不可用时，回退到基于 transcript 内容的时间窗口去重：
//   - 如果本次事件的 transcript 与上一次记录的 lastASRTranscript 完全相同，且距离上次记录时间
//     lastASRTranscriptAt 不超过 asrDuplicateWindow（2 秒），则认为是重复转写，返回 true（丢弃）。
//   - 否则将 lastASRTranscript 与 lastASRTranscriptAt 更新为当前值并返回 false（不丢弃）。
//
// 互斥与并发：
//   - 函数内部使用 g.asrDedupMu 保护对 lastASRResponseID/lastASRTranscript/lastASRTranscriptAt 的读写，
//     确保在并发 ASR 事件到达时不会发生竞态条件。
//
// 设计考量与例子：
//   - 常见情况 A：ASR 先发送 response.audio_transcript.done（含 response_id），随后发送 response.done。
//     两个事件会携带相同的 responseID，基于 responseID 的去重可以直接识别并丢弃第二次。
//   - 常见情况 B：某些 ASR 回调只包含 transcript 而没有 responseID（或 responseID 为空），
//     此时使用 transcript + 时间窗口（2s）去重能在短时间内合并重复上报，但不会无限期丢弃
//     与历史很早之前相同的文本。
//   - 为什么使用时间窗口：纯文本匹配容易误判（不同时间的相同短语可能是合法的新输入），
//     因此限制在短时间窗口内才认为是重复。
//
// 注意：该函数只负责决定是否丢弃事件；上层在遇到空 transcript 时已提前忽略，因此这里不需要
// 对空文本做额外判断（但保持覆盖性，当前实现也会正确处理空 transcript）。
func (g *MultiVoiceGateway) shouldDropASRResult(responseID string, transcript string) bool {
	g.asrDedupMu.Lock()
	defer g.asrDedupMu.Unlock()

	if responseID != "" {
		if responseID == g.lastASRResponseID {
			return true
		}
		g.lastASRResponseID = responseID
		g.lastASRTranscript = transcript
		g.lastASRTranscriptAt = time.Now()
		return false
	}

	if transcript != "" && transcript == g.lastASRTranscript {
		if time.Since(g.lastASRTranscriptAt) <= asrDuplicateWindow {
			return true
		}
	}

	g.lastASRTranscript = transcript
	g.lastASRTranscriptAt = time.Now()
	return false
}

func (g *MultiVoiceGateway) logRealtimeError(scope string, event map[string]interface{}) {
	raw, err := json.Marshal(event)
	if err != nil {
		g.logger.Printf("[MultiVoiceGateway] %s error payload marshal failed: %v", scope, err)
		return
	}

	g.logger.Printf("[MultiVoiceGateway] %s error payload: %s", scope, string(raw))

	if errObj, ok := event["error"].(map[string]interface{}); ok {
		g.logger.Printf("[MultiVoiceGateway] %s error detail: type=%v code=%v message=%v",
			scope,
			errObj["type"],
			errObj["code"],
			errObj["message"],
		)
	}
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

	// 提取 responseID
	response, _ := event["response"].(map[string]interface{})
	responseID, _ := response["id"].(string)

	// 清除活跃响应
	conn, _ := g.voicePool.GetRoleConn(g.ctx, role)
	if conn != nil {
		conn.ClearActiveResponse()
	}

	// 清除正在说话的角色
	g.voicePool.ClearSpeakingRole()

	// 注销元数据
	if responseID != "" {
		g.metadataRegistry.Unregister(responseID)
		g.logger.Printf("[MultiVoiceGateway] ✅ Unregistered metadata for responseID=%s", responseID)
	}

	// 提取最终文本
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

		// 将最终文本发给前端（用于 UI 气泡/字幕）并回灌给 Orchestrator（用于 SessionState 归约，支撑角色轮转）。
		metadata := g.snapshotActiveMetadata(role)
		_ = g.sendToClient(&ServerMessage{
			Type:     EventTypeAssistantText,
			Text:     finalText,
			Metadata: metadata,
			ServerTS: time.Now(),
		})

		_ = g.forwardToOrchestrator(&ClientMessage{
			Type:     EventTypeAssistantText,
			EventID:  fmt.Sprintf("assistant_%d", time.Now().UnixNano()),
			Text:     finalText,
			Metadata: metadata,
			ClientTS: time.Now(),
		})
	}

	return nil
}

// snapshotActiveMetadata 获取角色的最新响应元数据
func (g *MultiVoiceGateway) snapshotActiveMetadata(role string) map[string]interface{} {
	// 从注册表获取该角色的最新元数据
	if rm, ok := g.metadataRegistry.GetByRole(role); ok {
		metadata := make(map[string]interface{})
		for k, v := range rm.Metadata {
			metadata[k] = v
		}
		// 确保包含 role
		metadata["role"] = role
		return metadata
	}

	// 降级：如果注册表中没有，返回基本信息
	return map[string]interface{}{
		"role": role,
	}
}

// SendInstructions 发送指令到指定角色的连接
func (g *MultiVoiceGateway) SendInstructions(ctx context.Context, instructions string, metadata map[string]interface{}) error {
	// 从 metadata 中提取角色
	role, ok := metadata["role"].(string)
	if !ok || role == "" {
		g.logger.Printf("[MultiVoiceGateway] ❌ role not specified in metadata: %+v", metadata)
		return fmt.Errorf("role not specified in metadata")
	}

	g.logger.Printf("[MultiVoiceGateway] Sending instructions to role %s (len=%d)", role, len(instructions))
	g.logger.Printf("[MultiVoiceGateway] Metadata: %+v", metadata)

	// 保存活跃元数据
	g.activeMetadataLock.Lock()
	g.activeMetadata = metadata
	g.activeMetadataLock.Unlock()

	// 在指定角色的连接上创建响应
	err := g.voicePool.CreateResponse(ctx, role, instructions, metadata)
	if err != nil {
		g.logger.Printf("[MultiVoiceGateway] ❌ Failed to create response for role %s: %v", role, err)
	}
	return err
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

// sendTTSStartedToClient 发送 TTS 开始事件给客户端（包含角色信息）
func (g *MultiVoiceGateway) sendTTSStartedToClient(role string) {
	metadata := g.snapshotActiveMetadata(role)

	g.logger.Printf("[MultiVoiceGateway] 📤 Sending tts_started to client: role=%s", role)

	_ = g.sendToClient(&ServerMessage{
		Type:     "tts_started",
		Metadata: metadata,
		ServerTS: time.Now(),
	})
}

// sendTTSCompletedToClient 发送 TTS 完成事件给客户端
func (g *MultiVoiceGateway) sendTTSCompletedToClient(role string) {
	g.logger.Printf("[MultiVoiceGateway] 📤 Sending tts_completed to client: role=%s", role)

	_ = g.sendToClient(&ServerMessage{
		Type: "tts_completed",
		Metadata: map[string]interface{}{
			"role": role,
		},
		ServerTS: time.Now(),
	})
}

// SendQuizToClient 发送选择题到客户端
func (g *MultiVoiceGateway) SendQuizToClient(quizID, question string, options []string, context string) error {
	g.logger.Printf("[MultiVoiceGateway] 📤 Sending quiz to client: quiz_id=%s", quizID)

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

// Close 关闭网关
func (g *MultiVoiceGateway) Close() error {
	g.logger.Printf("[MultiVoiceGateway] Closing gateway for session %s", g.sessionID)

	g.closeOnce.Do(func() {
		g.cancel()
		close(g.closeChan)

		// 关闭事件队列（等待所有待处理事件完成）
		if g.eventQueue != nil {
			stats := g.eventQueue.GetStats()
			g.logger.Printf("[MultiVoiceGateway] Event queue stats: %+v", stats)
			if err := g.eventQueue.Close(); err != nil {
				g.logger.Printf("[MultiVoiceGateway] ⚠️  Error closing event queue: %v", err)
			}
		}

		// 清理元数据注册表
		if g.metadataRegistry != nil {
			g.metadataRegistry.Clear()
			g.logger.Printf("[MultiVoiceGateway] ✅ Metadata registry cleared")
		}

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
