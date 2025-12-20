package model

import "time"

// Bubble 定义了一个对话泡泡的基本信息。
type Bubble struct {
	EntryID          string   `json:"entry_id"`
	Domain           string   `json:"domain"`
	Title            string   `json:"title"`
	Subtitle         string   `json:"subtitle"`
	Hook             string   `json:"hook"`
	PrimaryConceptID string   `json:"primary_concept_id"`
	Tag              string   `json:"tag"`
	Description      string   `json:"description"`
	Keywords         []string `json:"keywords"`
	Color            string   `json:"color"`
}

// Turn 表示对话中的一个轮次。
type Turn struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	TS   time.Time `json:"ts"`
}

// BranchQuestion 表示一个分支问题。
type BranchQuestion struct {
	QuestionID string `json:"question_id"`
	Prompt     string `json:"prompt"`
}

// SignalsSnapshot 捕获了用户交互的信号快照。
type SignalsSnapshot struct {
	LastUserLatencyMS int64 `json:"last_user_latency_ms"`
	LastUserChars     int   `json:"last_user_chars"`
}

// SessionState 保存了一个对话会话的状态信息。
type SessionState struct {
	// 唯一标识一个会话。
	SessionID string `json:"session_id"`
	// 关联的泡泡信息。
	EntryID string `json:"entry_id"`
	// 泡泡所属领域。
	Domain string `json:"domain"`

	// 对话的主要目标和结构信息。
	MainObjective string `json:"main_objective"`
	// 当前对话的章节或阶段。
	Act int `json:"act"`
	// 当前对话的节拍。
	Beat string `json:"beat"`
	// 当前对话的角色设定。
	PacingMode string `json:"pacing_mode"`

	// 用户的知识掌握情况。
	MasteryEstimate float64 `json:"mastery_estimate"`
	// 用户可能存在的误解标签。
	MisconceptionTags []string `json:"misconception_tags"`

	// 对话的时间跟踪。
	OutputClockSec int `json:"output_clock_sec"`
	// 上次输出时间戳。
	LastOutputAt time.Time `json:"last_output_at"`

	// 用户的心理状态指标。
	TensionLevel int `json:"tension_level"`
	// 用户的认知负荷指标。
	CognitiveLoad int `json:"cognitive_load"`

	// 当前的分支问题栈。
	QuestionStack []BranchQuestion `json:"question_stack"`
	// 用户交互的信号快照。
	Signals SignalsSnapshot `json:"signals"`
	// 对话的历史轮次。
	Turns []Turn `json:"turns"`
}

// Event 表示时间线中的一个事件。
type Event struct {
	// Seq 由后端分配的单调序号，用于回放与幂等。
	Seq int64 `json:"seq,omitempty"`
	// SessionID 由编排器补齐，客户端可不传。
	SessionID string `json:"session_id,omitempty"`
	// EventID 用于去重与重试幂等，客户端可传 UUID。
	EventID string `json:"event_id,omitempty"`
	// TurnID 关联一次用户/助手轮次，便于回放与审计。
	TurnID string `json:"turn_id,omitempty"`

	// Type 表示事件类型（asr_final/user_message/quiz_answer/assistant_text/...）。
	Type string `json:"type"`
	// Text 是语音最终转写或用户文本输入。
	Text string `json:"text,omitempty"`
	// QuestionID/Answer 承载测评/工具类事件。
	QuestionID string `json:"question_id,omitempty"`
	Answer     string `json:"answer,omitempty"`
	// ClientTS/ServerTS 用于对齐体验与回放，ServerTS 由后端补齐。
	ClientTS time.Time `json:"client_ts,omitempty"`
	ServerTS time.Time `json:"server_ts,omitempty"`
	// DirectorPlan 作为结构化事实事件，便于验收与回放。
	DirectorPlan *DirectorPlan `json:"director_plan,omitempty"`
}

// DirectorPlan 包含对话导演的计划细节。
type DirectorPlan struct {
	UserMindState     []string `json:"user_mind_state"`
	Intent            string   `json:"intent"`
	NextBeat          string   `json:"next_beat"`
	NextRole          string   `json:"next_role"`
	OutputAction      string   `json:"output_action"`
	TalkBurstLimitSec int      `json:"talk_burst_limit_sec"`
	TensionGoal       string   `json:"tension_goal"`
	LoadGoal          string   `json:"load_goal"`
	StackAction       string   `json:"stack_action"`
	Notes             string   `json:"notes"`
}

// QuizQuestion 表示一个测验问题。
type QuizQuestion struct {
	ID      string   `json:"id"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

// DiagnoseSet 包含一组测验问题。
type DiagnoseSet struct {
	Questions []QuizQuestion `json:"questions"`
}

// UserAction 定义了需要用户执行的操作。
type UserAction struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
}

// AssistantMessage 表示助手生成的消息。
type AssistantMessage struct {
	Text           string      `json:"text"`
	NeedUserAction *UserAction `json:"need_user_action,omitempty"`
	Quiz           any         `json:"quiz"`
}

// EventResponse 是事件响应的结构体。
type EventResponse struct {
	Assistant AssistantMessage `json:"assistant"`
	Debug     *DebugPayload    `json:"debug,omitempty"`
}

// DebugPayload 包含调试信息。
type DebugPayload struct {
	DirectorPlan DirectorPlan `json:"director_plan"`
}

// CreateSessionResponse 是创建会话的响应结构体。
type CreateSessionResponse struct {
	SessionID string       `json:"session_id"`
	State     SessionState `json:"state"`
	Diagnose  DiagnoseSet  `json:"diagnose"`
}
