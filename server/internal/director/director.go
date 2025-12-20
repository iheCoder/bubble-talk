package director

import (
	"bubble-talk/server/internal/model"
	"math/rand"
)

// DirectorEngine 负责选择下一个 Beat 和角色
// 第一阶段：简单随机选择（未来会接入 LLM 分析）
type DirectorEngine struct {
	availableRoles []string
	availableBeats []string
	rng            *rand.Rand
}

// NewDirectorEngine 创建导演引擎
func NewDirectorEngine(roles []string, beats []string) *DirectorEngine {
	if len(roles) == 0 {
		roles = []string{"host", "economist", "skeptic"}
	}
	if len(beats) == 0 {
		beats = []string{
			"reveal", "check", "deepen", "twist", "continue",
			"lens_shift", "feynman", "montage", "minigame", "exit_ticket",
		}
	}
	return &DirectorEngine{
		availableRoles: roles,
		availableBeats: beats,
		rng:            rand.New(rand.NewSource(rand.Int63())),
	}
}

// Decide 生成导演计划（第一阶段：简单规则）
func (d *DirectorEngine) Decide(state *model.SessionState, userInput string) model.DirectorPlan {
	nextBeat := d.selectNextBeat(state)
	return model.DirectorPlan{
		UserMindState:     d.inferUserMindState(state, userInput),
		Intent:            "clarify",
		NextBeat:          nextBeat,
		NextRole:          d.selectNextRole(state),
		OutputAction:      d.determineOutputAction(nextBeat),
		TalkBurstLimitSec: d.determineTalkBurstLimit(state),
		TensionGoal:       d.determineTensionGoal(state),
		LoadGoal:          d.determineLoadGoal(state),
		StackAction:       "maintain",
		Notes:             "随机选择（第一阶段）",
	}
}

func (d *DirectorEngine) selectNextRole(_ *model.SessionState) string {
	return d.availableRoles[d.rng.Intn(len(d.availableRoles))]
}

func (d *DirectorEngine) selectNextBeat(state *model.SessionState) string {
	if state.OutputClockSec >= 90 {
		outputBeats := []string{"check", "feynman", "exit_ticket"}
		return outputBeats[d.rng.Intn(len(outputBeats))]
	}
	if len(state.MisconceptionTags) > 0 {
		correctionBeats := []string{"reveal", "twist", "lens_shift"}
		return correctionBeats[d.rng.Intn(len(correctionBeats))]
	}
	return d.availableBeats[d.rng.Intn(len(d.availableBeats))]
}

func (d *DirectorEngine) determineOutputAction(beat string) string {
	actionMap := map[string]string{
		"reveal": "explain_with_metaphor", "check": "ask_simple_question",
		"deepen": "ask_elaboration", "twist": "challenge_assumption",
		"continue": "acknowledge_and_continue", "lens_shift": "reframe_perspective",
		"feynman": "ask_teach_back", "montage": "show_multiple_examples",
		"minigame": "engage_interactive", "exit_ticket": "assess_transfer",
	}
	if action, ok := actionMap[beat]; ok {
		return action
	}
	return "continue_dialogue"
}

func (d *DirectorEngine) determineTalkBurstLimit(state *model.SessionState) int {
	if state.CognitiveLoad > 7 || state.TensionLevel > 7 {
		return 15
	}
	return 20
}

func (d *DirectorEngine) determineTensionGoal(state *model.SessionState) string {
	if state.TensionLevel < 4 {
		return "increase"
	} else if state.TensionLevel > 7 {
		return "decrease"
	}
	return "maintain"
}

func (d *DirectorEngine) determineLoadGoal(state *model.SessionState) string {
	if state.CognitiveLoad < 4 {
		return "increase"
	} else if state.CognitiveLoad > 7 {
		return "decrease"
	}
	return "maintain"
}

func (d *DirectorEngine) inferUserMindState(state *model.SessionState, _ string) []string {
	states := make([]string, 0)
	if len(state.MisconceptionTags) > 0 {
		states = append(states, "confused", "fog")
	}
	if state.MasteryEstimate < 0.3 {
		states = append(states, "novice")
	} else if state.MasteryEstimate > 0.7 {
		states = append(states, "confident")
	}
	if state.CognitiveLoad > 7 {
		states = append(states, "overwhelmed")
	}
	if len(states) == 0 {
		states = append(states, "engaged")
	}
	return states
}
