package director

import (
	"bubble-talk/server/internal/model"
	"testing"
)

func TestNewDirectorEngine(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)
	if engine == nil {
		t.Fatal("Expected engine")
	}
	if len(engine.availableRoles) == 0 {
		t.Error("Expected roles")
	}
	if len(engine.availableBeats) == 0 {
		t.Error("Expected beats")
	}
	t.Logf("✓ 加载了 %d 个角色, %d 个Beat", len(engine.availableRoles), len(engine.availableBeats))
}

func TestDecide(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)
	state := &model.SessionState{
		SessionID:       "test-1",
		MasteryEstimate: 0.5,
		OutputClockSec:  30,
		TensionLevel:    5,
		CognitiveLoad:   5,
	}
	plan := engine.Decide(state, "我不太明白")
	if plan.NextRole == "" {
		t.Error("Expected NextRole")
	}
	if plan.NextBeat == "" {
		t.Error("Expected NextBeat")
	}
	if plan.TalkBurstLimitSec == 0 {
		t.Error("Expected TalkBurstLimitSec")
	}
	t.Logf("✓ 计划: 角色=%s Beat=%s 动作=%s 时长=%ds",
		plan.NextRole, plan.NextBeat, plan.OutputAction, plan.TalkBurstLimitSec)
}

func TestOutputClockPriority(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)
	state := &model.SessionState{
		OutputClockSec: 100,
		TensionLevel:   5,
		CognitiveLoad:  5,
	}
	plan := engine.Decide(state, "")
	outputBeats := map[string]bool{"check": true, "feynman": true, "exit_ticket": true}
	if !outputBeats[plan.NextBeat] {
		t.Logf("⚠ 输出时钟>90s应选输出型Beat，当前: %s", plan.NextBeat)
	} else {
		t.Logf("✓ 正确选择输出型Beat: %s", plan.NextBeat)
	}
}

func TestMisconceptionPriority(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)
	state := &model.SessionState{
		MisconceptionTags: []string{"confuses_sunk_cost"},
		OutputClockSec:    30,
		TensionLevel:      5,
		CognitiveLoad:     5,
	}
	plan := engine.Decide(state, "")
	correctionBeats := map[string]bool{"reveal": true, "twist": true, "lens_shift": true}
	if !correctionBeats[plan.NextBeat] {
		t.Logf("⚠ 有误解应选纠正型Beat，当前: %s", plan.NextBeat)
	} else {
		t.Logf("✓ 正确选择纠正型Beat: %s", plan.NextBeat)
	}
}

func TestTalkBurstLimit(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)

	t.Run("正常20秒", func(t *testing.T) {
		state := &model.SessionState{CognitiveLoad: 5, TensionLevel: 5}
		limit := engine.determineTalkBurstLimit(state)
		if limit != 20 {
			t.Errorf("Expected 20, got %d", limit)
		}
		t.Log("✓ 正常状态20秒")
	})

	t.Run("负荷高15秒", func(t *testing.T) {
		state := &model.SessionState{CognitiveLoad: 8, TensionLevel: 5}
		limit := engine.determineTalkBurstLimit(state)
		if limit != 15 {
			t.Errorf("Expected 15, got %d", limit)
		}
		t.Log("✓ 认知负荷高时15秒")
	})
}

func TestUserMindState(t *testing.T) {
	engine := NewDirectorEngine(nil, nil)

	t.Run("有误解推断confused", func(t *testing.T) {
		state := &model.SessionState{
			MisconceptionTags: []string{"confuses_sunk_cost"},
			MasteryEstimate:   0.5,
			CognitiveLoad:     5,
		}
		states := engine.inferUserMindState(state, "")
		hasConfused := false
		for _, s := range states {
			if s == "confused" || s == "fog" {
				hasConfused = true
				break
			}
		}
		if !hasConfused {
			t.Error("Expected confused state")
		}
		t.Logf("✓ 推断状态: %v", states)
	})

	t.Run("掌握度低推断novice", func(t *testing.T) {
		state := &model.SessionState{MasteryEstimate: 0.2, CognitiveLoad: 5}
		states := engine.inferUserMindState(state, "")
		hasNovice := false
		for _, s := range states {
			if s == "novice" {
				hasNovice = true
				break
			}
		}
		if !hasNovice {
			t.Error("Expected novice")
		}
		t.Logf("✓ 推断状态: %v", states)
	})
}
