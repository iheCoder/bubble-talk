package model

import "time"

type Bubble struct {
	EntryID          string `json:"entry_id"`
	Domain           string `json:"domain"`
	Title            string `json:"title"`
	Hook             string `json:"hook"`
	PrimaryConceptID string `json:"primary_concept_id"`
}

type Turn struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	TS   time.Time `json:"ts"`
}

type BranchQuestion struct {
	QuestionID string `json:"question_id"`
	Prompt     string `json:"prompt"`
}

type SignalsSnapshot struct {
	LastUserLatencyMS int64 `json:"last_user_latency_ms"`
	LastUserChars     int   `json:"last_user_chars"`
}

type SessionState struct {
	SessionID string `json:"session_id"`
	EntryID   string `json:"entry_id"`
	Domain    string `json:"domain"`

	MainObjective string `json:"main_objective"`
	Act           int    `json:"act"`
	Beat          string `json:"beat"`
	PacingMode    string `json:"pacing_mode"`

	MasteryEstimate   float64  `json:"mastery_estimate"`
	MisconceptionTags []string `json:"misconception_tags"`

	OutputClockSec int       `json:"output_clock_sec"`
	LastOutputAt   time.Time `json:"last_output_at"`

	TensionLevel  int `json:"tension_level"`
	CognitiveLoad int `json:"cognitive_load"`

	QuestionStack []BranchQuestion `json:"question_stack"`
	Signals       SignalsSnapshot  `json:"signals"`
	Turns         []Turn           `json:"turns"`
}

type Event struct {
	Type       string    `json:"type"`
	Text       string    `json:"text,omitempty"`
	QuestionID string    `json:"question_id,omitempty"`
	Answer     string    `json:"answer,omitempty"`
	ClientTS   time.Time `json:"client_ts"`
}

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

type QuizQuestion struct {
	ID      string   `json:"id"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

type DiagnoseSet struct {
	Questions []QuizQuestion `json:"questions"`
}

type UserAction struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
}

type AssistantMessage struct {
	Text           string      `json:"text"`
	NeedUserAction *UserAction `json:"need_user_action,omitempty"`
	Quiz           any         `json:"quiz"`
}

type EventResponse struct {
	Assistant AssistantMessage `json:"assistant"`
	Debug     *DebugPayload    `json:"debug,omitempty"`
}

type DebugPayload struct {
	DirectorPlan DirectorPlan `json:"director_plan"`
}

type CreateSessionResponse struct {
	SessionID string       `json:"session_id"`
	State     SessionState `json:"state"`
	Diagnose  DiagnoseSet  `json:"diagnose"`
}
