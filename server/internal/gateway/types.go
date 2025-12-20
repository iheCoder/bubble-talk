package gateway

import (
	"time"
)

// EventType 定义了网关处理的事件类型
type EventType string

const (
	// 语音相关事件
	EventTypeAudioFrame     EventType = "audio_frame"     // 音频帧（用户上行）
	EventTypeASRPartial     EventType = "asr_partial"     // 部分转写（实时反馈）
	EventTypeASRFinal       EventType = "asr_final"       // 最终转写（触发决策）
	EventTypeTTSStarted     EventType = "tts_started"     // TTS开始播放
	EventTypeTTSCompleted   EventType = "tts_completed"   // TTS完成播放
	EventTypeTTSInterrupted EventType = "tts_interrupted" // TTS被打断

	// 工具/控制事件
	EventTypeQuizAnswer    EventType = "quiz_answer"    // 答题
	EventTypeBargeIn       EventType = "barge_in"       // 插话中断
	EventTypeExitRequested EventType = "exit_requested" // 退出请求

	// 系统/导演事件
	EventTypeDirectorPlan  EventType = "director_plan"  // 导演计划（内部）
	EventTypeAssistantText EventType = "assistant_text" // 助手文本输出
	EventTypeInstructions  EventType = "instructions"   // 向Realtime发送指令
)

// ClientMessage 客户端发送给网关的消息（WebSocket文本帧）
type ClientMessage struct {
	Type       EventType              `json:"type"`
	EventID    string                 `json:"event_id,omitempty"`    // 幂等去重
	TurnID     string                 `json:"turn_id,omitempty"`     // 轮次关联
	Text       string                 `json:"text,omitempty"`        // 文本输入（降级路径）
	QuestionID string                 `json:"question_id,omitempty"` // 答题相关
	Answer     string                 `json:"answer,omitempty"`      // 答题答案
	Metadata   map[string]interface{} `json:"metadata,omitempty"`    // 扩展字段
	ClientTS   time.Time              `json:"client_ts,omitempty"`   // 客户端时间戳
}

// ServerMessage 网关发送给客户端的消息
type ServerMessage struct {
	Type      EventType              `json:"type"`
	Seq       int64                  `json:"seq,omitempty"`        // 服务端序号
	TurnID    string                 `json:"turn_id,omitempty"`    // 轮次关联
	Text      string                 `json:"text,omitempty"`       // 文本内容
	AudioData []byte                 `json:"audio_data,omitempty"` // 音频数据（Base64编码）
	Metadata  map[string]interface{} `json:"metadata,omitempty"`   // 扩展字段
	ServerTS  time.Time              `json:"server_ts"`            // 服务端时间戳
	Error     string                 `json:"error,omitempty"`      // 错误信息
}

// RealtimeEvent OpenAI Realtime API 事件（WebSocket双向通信）
// 完整事件类型参考：https://platform.openai.com/docs/api-reference/realtime
type RealtimeEvent struct {
	Type    string                 `json:"type"`
	EventID string                 `json:"event_id,omitempty"`
	Payload map[string]interface{} `json:"-"` // 动态字段，由各event type决定
}

// RealtimeSessionUpdate 用于更新Realtime会话配置
type RealtimeSessionUpdate struct {
	Type    string                `json:"type"` // "session.update"
	Session RealtimeSessionConfig `json:"session"`
}

// RealtimeSessionConfig Realtime会话配置
type RealtimeSessionConfig struct {
	Modalities        []string             `json:"modalities,omitempty"`         // ["text", "audio"]
	Instructions      string               `json:"instructions,omitempty"`       // System prompt
	Voice             string               `json:"voice,omitempty"`              // alloy/echo/shimmer
	InputAudioFormat  string               `json:"input_audio_format,omitempty"` // pcm16/g711_ulaw/g711_alaw
	OutputAudioFormat string               `json:"output_audio_format,omitempty"`
	TurnDetection     *TurnDetectionConfig `json:"turn_detection,omitempty"` // VAD配置
	Tools             []interface{}        `json:"tools,omitempty"`          // Function calling（第一阶段不用）
	Temperature       float64              `json:"temperature,omitempty"`
	MaxTokens         int                  `json:"max_tokens,omitempty"`
}

// TurnDetectionConfig VAD（语音活动检测）配置
type TurnDetectionConfig struct {
	Type              string  `json:"type"`                          // "server_vad"
	Threshold         float64 `json:"threshold,omitempty"`           // 0.0-1.0
	PrefixPaddingMS   int     `json:"prefix_padding_ms,omitempty"`   // 开始前填充
	SilenceDurationMS int     `json:"silence_duration_ms,omitempty"` // 静音多久算结束
}

// RealtimeResponseCreate 创建回复指令（手动控制）
type RealtimeResponseCreate struct {
	Type     string                       `json:"type"` // "response.create"
	Response RealtimeResponseCreateConfig `json:"response,omitempty"`
}

// RealtimeResponseCreateConfig 回复创建配置
type RealtimeResponseCreateConfig struct {
	Modalities   []string `json:"modalities,omitempty"`   // ["text", "audio"]
	Instructions string   `json:"instructions,omitempty"` // 动态注入的导演指令
	Voice        string   `json:"voice,omitempty"`
	Temperature  float64  `json:"temperature,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
}

// RealtimeResponseCancel 取消当前回复（插话中断时使用）
type RealtimeResponseCancel struct {
	Type       string `json:"type"`                  // "response.cancel"
	ResponseID string `json:"response_id,omitempty"` // 可选，不传则取消所有进行中的
}

// RealtimeInputAudioBufferAppend 追加音频数据
type RealtimeInputAudioBufferAppend struct {
	Type  string `json:"type"`  // "input_audio_buffer.append"
	Audio string `json:"audio"` // Base64编码的音频数据
}

// RealtimeInputAudioBufferCommit 提交音频缓冲区（触发转写）
type RealtimeInputAudioBufferCommit struct {
	Type string `json:"type"` // "input_audio_buffer.commit"
}

// RealtimeInputAudioBufferClear 清空音频缓冲区
type RealtimeInputAudioBufferClear struct {
	Type string `json:"type"` // "input_audio_buffer.clear"
}

// RealtimeConversationItemCreate 创建对话项（手动注入消息）
type RealtimeConversationItemCreate struct {
	Type string                   `json:"type"` // "conversation.item.create"
	Item RealtimeConversationItem `json:"item"`
}

// RealtimeConversationItem 对话项
type RealtimeConversationItem struct {
	Type    string                `json:"type"`              // "message"/"function_call"/"function_call_output"
	Role    string                `json:"role,omitempty"`    // "user"/"assistant"/"system"
	Content []RealtimeContentPart `json:"content,omitempty"` // 内容部分
}

// RealtimeContentPart 内容部分
type RealtimeContentPart struct {
	Type       string `json:"type"`                 // "input_text"/"input_audio"/"text"/"audio"
	Text       string `json:"text,omitempty"`       // 文本内容
	Audio      string `json:"audio,omitempty"`      // Base64音频
	Transcript string `json:"transcript,omitempty"` // 音频转写
}
