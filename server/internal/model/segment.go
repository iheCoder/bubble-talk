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

// SegmentPlan 片段计划（导演的输出）- 极简设计
type SegmentPlan struct {
	SegmentID string `json:"segment_id"`

	// === 选角 ===
	// 这一段由哪个角色主导
	RoleID string `json:"role_id"`

	// === 剧情戏份（这是核心！）===
	// 导演给这个角色的具体戏份描述：
	// - 要说什么样的内容（不是逐字稿，是方向和要点）
	// - 用什么方式说（幽默/严肃、比喻/直接、引导/挑战）
	// - 要达成什么效果（制造好奇/澄清误解/推进理解）
	// 例如："用一个反直觉的例子（周末加班800块），制造'赚了还是亏了'的冲突，
	//       引导用户思考机会成本，说完后停下来等用户反应"
	SceneDirection string `json:"scene_direction"`

	// === 控制 ===
	// 自治预算（允许这个角色说多久）
	MaxDurationSec int `json:"max_duration_sec"`

	// === 元信息 ===
	// 导演的决策说明（调试用）
	DirectorNotes string `json:"director_notes,omitempty"`
}

// SegmentSnapshot 片段执行快照
type SegmentSnapshot struct {
	SegmentID  string    `json:"segment_id"`
	RoleID     string    `json:"role_id"`
	StartedAt  time.Time `json:"started_at"`
	ElapsedSec int       `json:"elapsed_sec"`
	Status     string    `json:"status"` // RUNNING, COMPLETED, INTERRUPTED
}
