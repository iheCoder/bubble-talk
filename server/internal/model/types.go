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
	Roles            []string `json:"roles"` // 这个泡泡使用的角色列表
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
	// 这个泡泡可用的角色列表（从 Bubble.Roles 复制过来）
	AvailableRoles []string `json:"available_roles"`

	// 对话的主要目标和结构信息。
	MainObjective string `json:"main_objective"`
	// 当前对话的章节或阶段。
	Act int `json:"act"`
	// 当前对话的节拍。
	Beat string `json:"beat"`
	// 当前对话的角色设定。
	PacingMode string `json:"pacing_mode"`

	// 剧本状态（新增）
	Script *ScriptState `json:"script,omitempty"`

	// 当前片段状态（新增）
	CurrentSegment *SegmentSnapshot `json:"current_segment,omitempty"`

	// 用户的知识掌握情况。
	MasteryEstimate float64 `json:"mastery_estimate"`
	// 用户可能存在的误解标签。
	MisconceptionTags []string `json:"misconception_tags"`

	// 对话的时间跟踪。
	OutputClockSec int `json:"output_clock_sec"`
	// 上次输出时间戳。
	LastOutputAt time.Time `json:"last_output_at"`
	// 上次有效输出时间（新增，用于判断是否需要强制窗口）
	LastEffectiveOutputSec int `json:"last_effective_output_sec,omitempty"`

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

	// 新增字段
	LastUserUtterance string    `json:"last_user_utterance,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TimelineEvent 时间线事件（用于Orchestrator）
type TimelineEvent struct {
	EventID   string                 `json:"event_id"`
	SessionID string                 `json:"session_id"`
	EventType string                 `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
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
	// 用户心理状态：Fog|Illusion|Partial|Aha|Verify|Expand|Fatigue
	UserMindState []string `json:"user_mind_state"`
	// 流动模式：FLOW（顺流）或 RESCUE（救场）
	FlowMode string `json:"flow_mode"`
	// 意图：Clarify|Deepen|Branch|Meta|OffTopic|Continue
	Intent string `json:"intent"`
	// 下一个拍点
	NextBeat string `json:"next_beat"`
	// 下一个角色
	NextRole string `json:"next_role"`
	// 输出动作
	OutputAction string `json:"output_action"`
	// 用户必须做的事
	UserMustDo *UserMustDo `json:"user_must_do,omitempty"`
	// 说话时间限制（秒）
	TalkBurstLimitSec int `json:"talk_burst_limit_sec"`
	// 张力目标
	TensionGoal string `json:"tension_goal"`
	// 负荷目标
	LoadGoal string `json:"load_goal"`
	// 栈操作
	StackAction string `json:"stack_action"`
	// 给工程调试的说明
	Notes string `json:"notes"`
	// 调试信息
	Debug *DirectorDebug `json:"debug,omitempty"`
}

// UserMustDo 定义用户必须执行的输出
type UserMustDo struct {
	// 类型：teach_back|choice|example|boundary|transfer
	Type string `json:"type"`
	// 给用户的具体要求提示
	Prompt string `json:"prompt"`
}

// DirectorDebug 导演决策的调试信息
type DirectorDebug struct {
	// 候选拍点列表
	BeatCandidates []string `json:"beat_candidates,omitempty"`
	// 选择该拍点的理由
	BeatChoiceReason string `json:"beat_choice_reason,omitempty"`
	// 角色选择理由
	RoleChoiceReason string `json:"role_choice_reason,omitempty"`
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
