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

	// 工具注册表（支持function calling）
	toolRegistry *tool.ToolRegistry

	// 状态管理
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeChan chan struct{}

	// 当前响应的元数据（角色、Beat等）
	activeMetadata     map[string]interface{}
	activeMetadataLock sync.RWMutex

	// ASR 去重（避免 response.done 与 response.audio_transcript.done 重复触发）
	asrDedupMu          sync.Mutex
	lastASRResponseID   string
	lastASRTranscript   string
	lastASRTranscriptAt time.Time

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
func (g *MultiVoiceGateway) forwardToOrchestrator(msg *ClientMessage) error {
	if g.eventHandler == nil {
		g.logger.Printf("[MultiVoiceGateway] ⚠️  no event handler set, dropping event: %s", msg.Type)
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] Forwarding event to Orchestrator: type=%s text=%s", msg.Type, msg.Text)

	go func() {
		ctx, cancel := context.WithTimeout(g.ctx, 10*time.Second)
		defer cancel()

		if err := g.eventHandler(ctx, msg); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ❌ Orchestrator handler error: %v", err)
			// 发送错误给客户端
			g.sendErrorToClient(fmt.Sprintf("Orchestrator error: %v", err))
		} else {
			g.logger.Printf("[MultiVoiceGateway] ✅ Orchestrator handled event successfully")
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
	case "error":
		g.logRealtimeError("ASR", event)
		return nil

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

	case "response.created":
		// ASR 连接不应该创建 response，但由于API行为，它会创建
		// 我们需要从 response 中提取转写，然后取消 response
		responseID, _ := event["response"].(map[string]interface{})["id"].(string)
		if responseID != "" {
			g.logger.Printf("[MultiVoiceGateway] ⚠️ ASR created response %s (will extract transcription and cancel)", responseID)
			asrConn, _ := g.voicePool.GetASRConn()
			if asrConn != nil {
				asrConn.SetActiveResponse(responseID)
			}
		}

	case "response.audio_transcript.done", "response.done":
		// ASR response 完成，提取转写文本
		return g.handleASRResponseDone(event)

	case "response.audio_transcript.delta":
		// 忽略 ASR 的音频转写增量（我们只关心最终文本）
		return nil
	}

	return nil
}

// handleASRResponseDone 从 ASR response 中提取转写并取消 response
func (g *MultiVoiceGateway) handleASRResponseDone(event map[string]interface{}) error {
	// 从 response 中提取转写
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

	if g.shouldDropASRResult(responseID, transcript) {
		g.logger.Printf("[MultiVoiceGateway] ⚠️ Duplicate ASR transcript dropped (response_id=%s)", responseID)
		return nil
	}

	g.logger.Printf("[MultiVoiceGateway] 📝 ASR transcription: %s", transcript)

	// 取消 ASR response（我们不需要它的音频输出）
	asrConn, _ := g.voicePool.GetASRConn()
	if asrConn != nil {
		if err := asrConn.CancelResponse(); err != nil {
			g.logger.Printf("[MultiVoiceGateway] ⚠️ Failed to cancel ASR response: %v", err)
		}
	}

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
		// 响应创建 - 发送 tts_started 事件给前端
		responseID, _ := event["response"].(map[string]interface{})["id"].(string)
		conn, _ := g.voicePool.GetRoleConn(g.ctx, role)
		if conn != nil {
			conn.SetActiveResponse(responseID)
		}

		// 发送 tts_started 给前端，包含角色信息
		g.sendTTSStartedToClient(role)

	case "response.audio.delta":
		// 音频增量（转发给客户端）
		return g.handleAudioDelta(role, event)

	case "response.audio_transcript.delta":
		// 文本增量（可选：显示实时字幕）
		delta, _ := event["delta"].(string)
		g.logger.Printf("[MultiVoiceGateway] Role %s transcript delta: %s", role, delta)

	case "response.done":
		// 响应完成 - 发送 tts_completed 给前端
		g.sendTTSCompletedToClient(role)
		return g.handleResponseDone(role, event)
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

	// 清除活跃响应
	conn, _ := g.voicePool.GetRoleConn(g.ctx, role)
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

func (g *MultiVoiceGateway) snapshotActiveMetadata(role string) map[string]interface{} {
	g.activeMetadataLock.RLock()
	metadata := make(map[string]interface{})
	for k, v := range g.activeMetadata {
		metadata[k] = v
	}
	g.activeMetadataLock.RUnlock()

	metadata["role"] = role
	return metadata
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
