package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/model"
	"strings"
	"testing"
	"time"
)

// TestInferFlowMode 测试流动模式推断
func TestInferFlowMode(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	tests := []struct {
		name     string
		state    *model.SessionState
		input    string
		expected string
	}{
		{
			name: "有误解标签应该是 RESCUE",
			state: &model.SessionState{
				MisconceptionTags: []string{"M1_cost_equals_money_spent"},
				MasteryEstimate:   0.5,
				CognitiveLoad:     5,
				TensionLevel:      5,
			},
			input:    "我明白了",
			expected: "RESCUE",
		},
		{
			name: "掌握度低应该是 RESCUE",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				MasteryEstimate:   0.3,
				CognitiveLoad:     5,
				TensionLevel:      5,
			},
			input:    "继续",
			expected: "RESCUE",
		},
		{
			name: "认知负荷高应该是 RESCUE",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				MasteryEstimate:   0.6,
				CognitiveLoad:     8,
				TensionLevel:      5,
			},
			input:    "好的",
			expected: "RESCUE",
		},
		{
			name: "用户困惑应该是 RESCUE",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				MasteryEstimate:   0.6,
				CognitiveLoad:     5,
				TensionLevel:      5,
			},
			input:    "我不明白什么意思",
			expected: "RESCUE",
		},
		{
			name: "正常状态应该是 FLOW",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				MasteryEstimate:   0.6,
				CognitiveLoad:     5,
				TensionLevel:      5,
			},
			input:    "我理解了，继续吧",
			expected: "FLOW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := director.inferFlowMode(tt.state, tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestInferUserMindState 测试用户心理状态推断
func TestInferUserMindState(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	tests := []struct {
		name          string
		state         *model.SessionState
		input         string
		expectedState string // 期望包含的状态
	}{
		{
			name: "短输入+高延迟=Fatigue",
			state: &model.SessionState{
				Signals: model.SignalsSnapshot{
					LastUserChars:     5,
					LastUserLatencyMS: 6000,
				},
				MasteryEstimate: 0.5,
				CognitiveLoad:   5,
				TensionLevel:    5,
			},
			input:         "嗯",
			expectedState: "Fatigue",
		},
		{
			name: "有误解+高负荷=Fog",
			state: &model.SessionState{
				MisconceptionTags: []string{"M1"},
				Signals: model.SignalsSnapshot{
					LastUserChars:     50,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.3,
				CognitiveLoad:   7,
				TensionLevel:    5,
			},
			input:         "我不太懂",
			expectedState: "Fog",
		},
		{
			name: "低掌握+低张力=Illusion",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				Signals: model.SignalsSnapshot{
					LastUserChars:     20,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.3,
				CognitiveLoad:   5,
				TensionLevel:    3,
			},
			input:         "懂了懂了",
			expectedState: "Illusion",
		},
		{
			name: "中等掌握=Partial",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				Signals: model.SignalsSnapshot{
					LastUserChars:     50,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.5,
				CognitiveLoad:   5,
				TensionLevel:    5,
			},
			input:         "我大概理解了",
			expectedState: "Partial",
		},
		{
			name: "高掌握+无误解=Aha",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				Signals: model.SignalsSnapshot{
					LastUserChars:     50,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.8,
				CognitiveLoad:   5,
				TensionLevel:    5,
			},
			input:         "原来如此！",
			expectedState: "Aha",
		},
		{
			name: "带问号=Verify",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				Signals: model.SignalsSnapshot{
					LastUserChars:     30,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.6,
				CognitiveLoad:   5,
				TensionLevel:    5,
			},
			input:         "那如果是另一种情况呢？",
			expectedState: "Verify",
		},
		{
			name: "提到例子=Expand",
			state: &model.SessionState{
				MisconceptionTags: []string{},
				Signals: model.SignalsSnapshot{
					LastUserChars:     40,
					LastUserLatencyMS: 2000,
				},
				MasteryEstimate: 0.6,
				CognitiveLoad:   5,
				TensionLevel:    5,
			},
			input:         "比如说在公司里会怎样",
			expectedState: "Expand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := director.inferUserMindState(tt.state, tt.input)
			found := false
			for _, state := range result {
				if state == tt.expectedState {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected to find %s in %v", tt.expectedState, result)
			}
		})
	}
}

// TestGenerateBeatCandidates 测试拍点候选生成
func TestGenerateBeatCandidates(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	tests := []struct {
		name           string
		state          *model.SessionState
		flowMode       string
		userMindState  []string
		expectedInList []string // 期望包含的拍点
	}{
		{
			name: "超过时钟阈值应该强制输出型Beat",
			state: &model.SessionState{
				OutputClockSec: 95,
			},
			flowMode:       "FLOW",
			userMindState:  []string{"Engaged"},
			expectedInList: []string{"check", "feynman", "exit_ticket"},
		},
		{
			name:           "Fatigue状态应该降低负荷",
			state:          &model.SessionState{OutputClockSec: 30},
			flowMode:       "RESCUE",
			userMindState:  []string{"Fatigue"},
			expectedInList: []string{"minigame", "exit_ticket"},
		},
		{
			name:           "FLOW模式应该小步推进",
			state:          &model.SessionState{OutputClockSec: 30},
			flowMode:       "FLOW",
			userMindState:  []string{"Engaged"},
			expectedInList: []string{"continue", "deepen", "check"},
		},
		{
			name:           "Fog状态应该简化解释",
			state:          &model.SessionState{OutputClockSec: 30},
			flowMode:       "RESCUE",
			userMindState:  []string{"Fog"},
			expectedInList: []string{"reveal", "lens_shift"},
		},
		{
			name:           "Illusion状态应该戳破误解",
			state:          &model.SessionState{OutputClockSec: 30},
			flowMode:       "RESCUE",
			userMindState:  []string{"Illusion"},
			expectedInList: []string{"twist", "check"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := director.generateBeatCandidates(tt.state, tt.flowMode, tt.userMindState)

			// 检查是否包含期望的拍点
			for _, expected := range tt.expectedInList {
				found := false
				for _, candidate := range result {
					if candidate == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %s in candidates %v", expected, result)
				}
			}
		})
	}
}

// TestDecideWithRules 测试规则引擎决策
func TestDecideWithRules(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "continue"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	state := &model.SessionState{
		SessionID:       "test-session",
		EntryID:         "econ_offer",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.5,
		OutputClockSec:  30,
		TensionLevel:    5,
		CognitiveLoad:   5,
		Turns: []model.Turn{
			{Role: "user", Text: "我想了解机会成本", TS: time.Now()},
		},
	}

	flowMode := "FLOW"
	userMindState := []string{"Partial"}
	beatCandidates := []string{"continue", "check", "deepen"}

	plan := director.decideWithRules(state, flowMode, userMindState, beatCandidates)

	// 验证计划结构
	if plan.FlowMode != flowMode {
		t.Errorf("expected flow_mode %s, got %s", flowMode, plan.FlowMode)
	}
	if plan.NextBeat == "" {
		t.Error("next_beat should not be empty")
	}
	if plan.NextRole == "" {
		t.Error("next_role should not be empty")
	}
	if plan.TalkBurstLimitSec <= 0 {
		t.Error("talk_burst_limit_sec should be positive")
	}
	if plan.Debug == nil {
		t.Error("debug info should not be nil")
	}
}

// TestApplyGuardrails 测试硬约束验证
func TestApplyGuardrails(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			AvailableRoles:         []string{"host", "economist"},
			AvailableBeats:         []string{"reveal", "check", "deepen"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	state := &model.SessionState{
		AvailableRoles: []string{"host", "economist"},
	}

	tests := []struct {
		name         string
		plan         decisionPlan
		expectedBeat string
		expectedRole string
	}{
		{
			name: "无效拍点应该回退到check",
			plan: decisionPlan{
				NextBeat: "invalid_beat",
				NextRole: "host",
			},
			expectedBeat: "check",
			expectedRole: "host",
		},
		{
			name: "无效角色应该回退到第一个可用角色",
			plan: decisionPlan{
				NextBeat: "check",
				NextRole: "invalid_role",
			},
			expectedBeat: "check",
			expectedRole: "host",
		},
		{
			name: "有效计划应该保持不变",
			plan: decisionPlan{
				NextBeat: "reveal",
				NextRole: "economist",
			},
			expectedBeat: "reveal",
			expectedRole: "economist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := director.applyGuardrails(tt.plan, state)

			if result.NextBeat != tt.expectedBeat {
				t.Errorf("expected beat %s, got %s", tt.expectedBeat, result.NextBeat)
			}

			if result.NextRole != tt.expectedRole {
				t.Errorf("expected role %s, got %s", tt.expectedRole, result.NextRole)
			}
		})
	}
}

// TestRoleRotation 测试角色轮换
func TestRoleRotation(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	state := &model.SessionState{
		AvailableRoles: []string{"host", "economist", "skeptic"},
		Turns: []model.Turn{
			{Role: "user", Text: "问题1", TS: time.Now()},
			{Role: "assistant", Text: "回答1", TS: time.Now()}, // 第1个assistant turn
			{Role: "user", Text: "问题2", TS: time.Now()},
			{Role: "assistant", Text: "回答2", TS: time.Now()}, // 第2个assistant turn
			{Role: "user", Text: "问题3", TS: time.Now()},
		},
	}

	// 第3个assistant turn应该选择第3个角色（index 2）
	role := director.selectNextRole(state)

	// assistantTurns = 2, 2 % 3 = 2, 应该选择 skeptic
	if role != "skeptic" {
		t.Errorf("expected 'skeptic', got '%s'", role)
	}
}

// TestDetermineTalkBurstLimit 测试说话时长限制
func TestDetermineTalkBurstLimit(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	tests := []struct {
		name     string
		state    *model.SessionState
		expected int
	}{
		{
			name: "高认知负荷应该缩短时长",
			state: &model.SessionState{
				CognitiveLoad: 8,
				TensionLevel:  5,
			},
			expected: 15,
		},
		{
			name: "高张力应该缩短时长",
			state: &model.SessionState{
				CognitiveLoad: 5,
				TensionLevel:  8,
			},
			expected: 15,
		},
		{
			name: "正常状态应该使用默认时长",
			state: &model.SessionState{
				CognitiveLoad: 5,
				TensionLevel:  5,
			},
			expected: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := director.determineTalkBurstLimit(tt.state)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestBeatLibraryInitialization 测试拍点库初始化
func TestBeatLibraryInitialization(t *testing.T) {
	library := initBeatLibrary()

	requiredBeats := []string{
		"reveal", "check", "deepen", "twist", "continue",
		"lens_shift", "feynman", "montage", "minigame", "exit_ticket",
	}

	for _, beatID := range requiredBeats {
		card, ok := library[beatID]
		if !ok {
			t.Errorf("beat '%s' not found in library", beatID)
			continue
		}

		// 验证卡片字段
		if card.BeatID != beatID {
			t.Errorf("beat_id mismatch: expected %s, got %s", beatID, card.BeatID)
		}

		if card.Goal == "" {
			t.Errorf("beat '%s' has empty goal", beatID)
		}

		if card.UserMustDoType == "" {
			t.Errorf("beat '%s' has empty user_must_do_type", beatID)
		}

		if card.TalkBurstLimitHint <= 0 {
			t.Errorf("beat '%s' has invalid talk_burst_limit_hint: %d", beatID, card.TalkBurstLimitHint)
		}
	}
}

// TestEndToEndDecision 测试端到端决策流程（规则引擎）
func TestEndToEndDecision(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false, // 使用规则引擎
			AvailableRoles:         []string{"host", "economist"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "continue"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}
	director := NewDirectorEngine(cfg, nil)

	// 模拟一个完整的对话场景
	state := &model.SessionState{
		SessionID:         "test-session",
		EntryID:           "econ_opportunity_cost",
		AvailableRoles:    []string{"host", "economist"},
		MasteryEstimate:   0.4,
		OutputClockSec:    30,
		TensionLevel:      5,
		CognitiveLoad:     6,
		MisconceptionTags: []string{"M1_cost_equals_money_spent"},
		Signals: model.SignalsSnapshot{
			LastUserChars:     35,
			LastUserLatencyMS: 2500,
		},
		Turns: []model.Turn{
			{Role: "user", Text: "机会成本是什么？", TS: time.Now()},
			{Role: "assistant", Text: "让我用个例子来解释...", TS: time.Now()},
		},
	}

	userInput := "所以机会成本就是花掉的钱？"

	// 执行决策
	plan := director.Decide(state, userInput)

	// 验证决策结果
	if plan.NextRole == "" {
		t.Error("next_role should not be empty")
	}
	if plan.Instruction == "" {
		t.Error("instruction should not be empty")
	}
	if plan.Debug == nil {
		t.Error("debug info should not be nil")
	}

	// 由于有误解标签，应该是 RESCUE 模式
	if !strings.Contains(plan.Instruction, "Flow Mode: RESCUE") {
		t.Errorf("expected RESCUE mode for misconception, got instruction: %s", plan.Instruction)
	}

	// 验证角色在可用列表中
	if !contains(state.AvailableRoles, plan.NextRole) {
		t.Errorf("role %s not in available roles", plan.NextRole)
	}

	// 验证拍点在可用列表中
	foundBeat := false
	for _, beat := range cfg.Director.AvailableBeats {
		if strings.Contains(plan.Instruction, "Next Beat: "+beat) {
			foundBeat = true
			break
		}
	}
	if !foundBeat {
		t.Errorf("instruction should include an available beat, got: %s", plan.Instruction)
	}

	t.Logf("✅ Decision successful (Rule-based):")
	t.Logf("   NextRole: %s", plan.NextRole)
	t.Logf("   Instruction: %s", plan.Instruction)
}

// TestLLMDecision 测试 LLM 驱动的决策
func TestLLMDecision(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              true, // 启用 LLM
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue", "lens_shift", "feynman"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	// 使用 Mock LLM 客户端
	mockLLM := NewMockLLMClient()
	director := NewDirectorEngine(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test-llm-session",
		EntryID:         "econ_test",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.5,
		OutputClockSec:  30,
		TensionLevel:    5,
		CognitiveLoad:   5,
		Turns: []model.Turn{
			{Role: "user", Text: "我想理解机会成本", TS: time.Now()},
		},
	}

	userInput := "继续解释"

	// 执行决策
	plan := director.Decide(state, userInput)

	// 验证 LLM 被调用
	if mockLLM.CallCount == 0 {
		t.Error("LLM client should have been called")
	}

	// 验证决策结果结构完整
	if plan.NextRole == "" {
		t.Error("next_role should not be empty")
	}
	if plan.Instruction == "" {
		t.Error("instruction should not be empty")
	}

	t.Logf("✅ LLM Decision successful:")
	t.Logf("   LLM call count: %d", mockLLM.CallCount)
	t.Logf("   NextRole: %s", plan.NextRole)
	t.Logf("   Instruction: %s", plan.Instruction)
}

// TestLLMDecisionWithCustomResponse 测试 LLM 返回自定义决策
func TestLLMDecisionWithCustomResponse(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	mockLLM := NewMockLLMClient()

	// 设置自定义响应：twist 拍点用于打破错觉
	mockLLM.ResponsePlan = &struct {
		FlowMode          string   `json:"flow_mode"`
		UserMindState     []string `json:"user_mind_state"`
		Intent            string   `json:"intent"`
		NextBeat          string   `json:"next_beat"`
		NextRole          string   `json:"next_role"`
		OutputAction      string   `json:"output_action"`
		TalkBurstLimitSec int      `json:"talk_burst_limit_sec"`
		Notes             string   `json:"notes"`
		Debug             *struct {
			BeatCandidates   []string `json:"beat_candidates"`
			BeatChoiceReason string   `json:"beat_choice_reason"`
			RoleChoiceReason string   `json:"role_choice_reason"`
		} `json:"debug"`
	}{
		FlowMode:          "RESCUE",
		UserMindState:     []string{"Illusion"},
		Intent:            "clarify",
		NextBeat:          "twist",
		NextRole:          "skeptic",
		OutputAction:      "challenge_assumption",
		TalkBurstLimitSec: 20,
		Notes:             "用户假装理解，需要用反例打破错觉",
	}

	director := NewDirectorEngine(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test-custom-llm",
		AvailableRoles:  []string{"host", "economist", "skeptic"},
		MasteryEstimate: 0.2,
		OutputClockSec:  40,
		TensionLevel:    5,
		CognitiveLoad:   5,
		Turns: []model.Turn{
			{Role: "user", Text: "我完全懂了", TS: time.Now()},
		},
	}

	plan := director.Decide(state, "没问题")

	// 验证 LLM 返回的自定义决策被正确使用
	if plan.NextRole != "skeptic" {
		t.Errorf("expected role 'skeptic', got '%s'", plan.NextRole)
	}
	if !strings.Contains(plan.Instruction, "Next Beat: twist") {
		t.Errorf("expected instruction to include twist beat, got: %s", plan.Instruction)
	}
	if !strings.Contains(plan.Instruction, "Flow Mode: RESCUE") {
		t.Errorf("expected RESCUE mode, got instruction: %s", plan.Instruction)
	}
	if !strings.Contains(plan.Instruction, "User Mind State: Illusion") {
		t.Errorf("expected Illusion in instruction, got: %s", plan.Instruction)
	}

	t.Logf("✅ Custom LLM Response handled correctly:")
	t.Logf("   NextRole: %s (expected: skeptic)", plan.NextRole)
	t.Logf("   Instruction: %s", plan.Instruction)
}

// TestLLMDecisionFailoverToRules 测试 LLM 失败时降级到规则引擎
func TestLLMDecisionFailoverToRules(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "continue"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	mockLLM := NewMockLLMClient()
	mockLLM.ShouldFail = true // 让 LLM 返回错误

	director := NewDirectorEngine(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test-failover",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.5,
		OutputClockSec:  30,
		TensionLevel:    5,
		CognitiveLoad:   5,
		Turns: []model.Turn{
			{Role: "user", Text: "继续", TS: time.Now()},
		},
	}

	plan := director.Decide(state, "好的")

	// 验证即使 LLM 失败，决策仍然有效
	if plan.Instruction == "" {
		t.Error("decision should be made even when LLM fails (fallback to rules)")
	}
	if plan.NextRole == "" {
		t.Error("role should be selected even when LLM fails")
	}

	// 验证来自规则引擎的备注
	if !strings.Contains(plan.Instruction, "Notes: 规则引擎选择") {
		t.Logf("note: expected fallback to rules, got instruction: %s", plan.Instruction)
	}

	t.Logf("✅ LLM failure fallback works correctly:")
	t.Logf("   NextRole: %s (from rules)", plan.NextRole)
	t.Logf("   Instruction: %s", plan.Instruction)
}

// TestLLMDecisionCallCount 测试 LLM 调用次数
func TestLLMDecisionCallCount(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist"},
			AvailableBeats:         []string{"reveal", "check", "deepen"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	mockLLM := NewMockLLMClient()
	director := NewDirectorEngine(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test-call-count",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.5,
		OutputClockSec:  30,
		TensionLevel:    5,
		CognitiveLoad:   5,
	}

	// 执行多次决策
	for i := 0; i < 3; i++ {
		director.Decide(state, "input")
	}

	// 验证 LLM 被调用了正确的次数
	if mockLLM.CallCount != 3 {
		t.Errorf("expected 3 LLM calls, got %d", mockLLM.CallCount)
	}

	t.Logf("✅ LLM call count tracking works:")
	t.Logf("   Total calls: %d", mockLLM.CallCount)
}

// TestLLMDecisionWithTimeoutBeat 测试当 output_clock 超时时 LLM 的行为
func TestLLMDecisionWithTimeoutBeat(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist"},
			AvailableBeats:         []string{"check", "feynman", "exit_ticket"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	mockLLM := NewMockLLMClient()
	mockLLM.ResponsePlan = &struct {
		FlowMode          string   `json:"flow_mode"`
		UserMindState     []string `json:"user_mind_state"`
		Intent            string   `json:"intent"`
		NextBeat          string   `json:"next_beat"`
		NextRole          string   `json:"next_role"`
		OutputAction      string   `json:"output_action"`
		TalkBurstLimitSec int      `json:"talk_burst_limit_sec"`
		Notes             string   `json:"notes"`
		Debug             *struct {
			BeatCandidates   []string `json:"beat_candidates"`
			BeatChoiceReason string   `json:"beat_choice_reason"`
			RoleChoiceReason string   `json:"role_choice_reason"`
		} `json:"debug"`
	}{
		FlowMode:          "RESCUE",
		UserMindState:     []string{"Fatigue"},
		Intent:            "conclude",
		NextBeat:          "exit_ticket",
		NextRole:          "host",
		OutputAction:      "assess_transfer",
		TalkBurstLimitSec: 15,
		Notes:             "用户时间已到，启动测评",
	}

	director := NewDirectorEngine(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test-timeout",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.6,
		OutputClockSec:  95, // 超过 90 秒阈值
		TensionLevel:    5,
		CognitiveLoad:   5,
	}

	plan := director.Decide(state, "结束")

	// 验证超时时选择了输出型 Beat
	if !strings.Contains(plan.Instruction, "Next Beat: exit_ticket") {
		t.Errorf("expected exit_ticket for timeout, got instruction: %s", plan.Instruction)
	}

	t.Logf("✅ Output clock timeout handled correctly:")
	t.Logf("   Clock: %d sec (threshold: 90)", state.OutputClockSec)
	t.Logf("   Instruction: %s", plan.Instruction)
}
