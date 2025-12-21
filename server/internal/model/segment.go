package model

import "time"

// Script 剧本定义（故事文本，而不是结构化数据）
// 剧本是导演的"活文档"，会根据实际发生动态调整
type Script struct {
	ScriptID string `json:"script_id"`
	EntryID  string `json:"entry_id"`

	// 原始故事（初始剧本，不变）
	OriginalStory string `json:"original_story"`

	// 当前故事（会根据实际发生动态调整）
	// 包含：主题、冲突、推进点、收束方式等
	// LLM 可以直接理解和改写
	CurrentStory string `json:"current_story"`

	// 元信息
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   string    `json:"version"`
}

// ScriptState 剧本运行状态
type ScriptState struct {
	ScriptID string `json:"script_id"`

	// 对齐模式：导演对剧本的运行态度
	// FOLLOW: 贴近剧本走 (alignment > 0.7)
	// ADAPT: 保留主题但改写组织 (0.4 < alignment ≤ 0.7)
	// REWRITE: 剧本只作风格参考 (alignment ≤ 0.4)
	AlignmentMode string `json:"alignment_mode"`

	// 对齐评分：当前状态与剧本预期的一致性 (0-1)
	AlignmentScore float64 `json:"alignment_score"`

	// 已发生的故事摘要（记录实际发生了什么，用于判断剧本哪些部分已触发/被跳过）
	// 例如："用户已经提前理解了机会成本的核心，直接问了边界问题"
	StoryProgress string `json:"story_progress,omitempty"`

	// 剧本修改历史（重要的改写记录）
	Revisions []ScriptRevision `json:"revisions,omitempty"`

	LastAlignmentAt time.Time `json:"last_alignment_at"`
}

// ScriptRevision 剧本修订记录
type ScriptRevision struct {
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"` // 为什么改：如"用户提前触发了冲突环节"
	Change    string    `json:"change"` // 改了什么：简短描述
}

// SegmentPlan 片段计划（导演的输出）
// 这是导演基于剧本、已发生的故事、用户交互，决定的"这一段戏怎么演"
// 不是简单的"类型选择"，而是具体的剧情戏份和回应策略
type SegmentPlan struct {
	SegmentID string `json:"segment_id"`

	// === 选角 ===
	// 这一段由哪个角色主导
	RoleID string `json:"role_id"` // 如 "host", "economist"

	// === 剧情戏份（这是核心！）===
	// 导演给这个角色的具体戏份描述：
	// - 要说什么样的内容（不是逐字稿，是方向和要点）
	// - 用什么方式说（幽默/严肃、比喻/直接、引导/挑战）
	// - 要达成什么效果（制造好奇/澄清误解/推进理解）
	// 例如："用一个反直觉的例子（周末加班800块），制造'赚了还是亏了'的冲突，
	//       引导用户思考机会成本，说完后停下来等用户反应"
	SceneDirection string `json:"scene_direction"`

	// === 用户回应策略 ===
	// 如果用户刚刚有输入，这个角色应该如何回应
	// 包含对用户意图、心理状态的判断和回应方式
	UserResponseStrategy *UserResponseStrategy `json:"user_response_strategy,omitempty"`

	// === 叙事倾向 ===
	NarrativeTilt NarrativeTilt `json:"narrative_tilt"`

	// === 目标 ===
	// 这一段要达成的教学目标和用户输出要求
	SegmentGoal SegmentGoal `json:"segment_goal"`

	// === 控制 ===
	// 自治预算（允许这个角色说多久）
	AutonomyBudget AutonomyBudget `json:"autonomy_budget"`

	// 互动窗口（在哪些时机让用户参与）
	InteractionWindows []InteractionWindow `json:"interaction_windows"`

	// 护栏约束
	Guardrails Guardrails `json:"guardrails"`

	// === 元信息 ===
	// 导演的决策说明（调试用）
	DirectorNotes string `json:"director_notes"`

	// 剧本参考（这段戏在剧本中的位置/对应关系）
	ScriptReference string `json:"script_reference,omitempty"`
}

// UserResponseStrategy 用户回应策略
// 当用户有输入时，角色应该如何回应
type UserResponseStrategy struct {
	// 用户意图识别（导演的判断）
	UserIntent string `json:"user_intent"` // clarify, challenge, expand, off_topic, etc.

	// 用户心理状态（导演的判断）
	UserMindState []string `json:"user_mind_state"` // Fog, Partial, Aha, etc.

	// 回应方式
	// 例如："先肯定用户的观察，然后用一个更清晰的例子澄清误解"
	ResponseApproach string `json:"response_approach"`

	// 是否需要在回应后逼出用户输出
	NeedUserOutput bool `json:"need_user_output"`

	// 如果用户意图偏离主线，是否需要拉回
	NeedHookBack bool `json:"need_hook_back"`
}

// NarrativeTilt 叙事倾向
type NarrativeTilt struct {
	Mode          string `json:"mode"`           // INTERVIEW, DEBATE, STORY, MONTAGE
	Tone          string `json:"tone"`           // LIGHT, SERIOUS, PLAYFUL
	TeachingStyle string `json:"teaching_style"` // SOCRATIC, DIRECT, STORY_DRIVEN
}

// SegmentGoal 片段目标
type SegmentGoal struct {
	Teaching   string      `json:"teaching"`     // 教学目标
	UserMustDo *UserMustDo `json:"user_must_do"` // 用户必须完成的输出
}

// AutonomyBudget 自治预算
type AutonomyBudget struct {
	MaxSec   int `json:"max_sec"`   // 最大时长（秒）
	MaxTurns int `json:"max_turns"` // 最大轮次
}

// InteractionWindow 互动窗口
type InteractionWindow struct {
	WindowID   string      `json:"window_id"`
	Trigger    string      `json:"trigger"`      // AFTER_SETUP, BEFORE_WRAP, TIME_BASED
	MaxWaitSec int         `json:"max_wait_sec"` // 最长等待时间
	UserMustDo *UserMustDo `json:"user_must_do"`
}

// Guardrails 护栏约束
type Guardrails struct {
	MaxTotalOutputSec int      `json:"max_total_output_sec"`
	MustReference     []string `json:"must_reference"` // 必须引用的概念/标签
	DisallowNewRoles  bool     `json:"disallow_new_roles"`
}

// FallbackStrategy 降级策略
type FallbackStrategy struct {
	OnFatigue  *FallbackAction `json:"on_fatigue,omitempty"`
	OnOffTopic *FallbackAction `json:"on_off_topic,omitempty"`
	OnUserStop *FallbackAction `json:"on_user_stop,omitempty"`
}

// FallbackAction 降级动作
type FallbackAction struct {
	SegmentType string `json:"segment_type"`
}

// SegmentSnapshot 片段执行快照
type SegmentSnapshot struct {
	SegmentID      string    `json:"segment_id"`
	StartedAt      time.Time `json:"started_at"`
	ElapsedSec     int       `json:"elapsed_sec"`
	TurnsCompleted int       `json:"turns_completed"`
	WindowCursor   int       `json:"window_cursor"` // 当前窗口索引
	Status         string    `json:"status"`        // RUNNING, PAUSED, COMPLETED, INTERRUPTED
}
