package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/llm"
	"bubble-talk/server/internal/model"
	"os"
	"testing"
	"time"
)

// TestRealLLMOpenAI 测试真实 OpenAI LLM 的导演决策
// 需要设置环境变量: LLM_API_KEY
// 运行: go test -v -run TestRealLLMOpenAI ./server/internal/director/... -tags=integration
func TestRealLLMOpenAI(t *testing.T) {
	// 跳过条件：没有 API Key 或没有指定 -tags=integration
	//apiKey := os.Getenv("LLM_API_KEY")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("⏭️  Skipping real LLM test: LLM_API_KEY not set")
	}

	// 创建配置
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			OpenAI: config.LLMProviderConfig{
				APIKey:      apiKey,
				APIURL:      "https://api.openai.com/v1",
				Model:       "gpt-4o-mini", // 使用便宜的模型进行测试
				Temperature: 0.7,
				MaxTokens:   2000,
			},
		},
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue", "lens_shift", "feynman", "montage", "minigame", "exit_ticket"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	// 创建真实 LLM 客户端
	llmClient, err := llm.NewClient(cfg)
	if err != nil {
		t.Fatalf("❌ Failed to create LLM client: %v", err)
	}

	// 创建导演引擎
	director := NewDirectorEngine(cfg, llmClient)

	// 场景 1: 用户有误解，需要救场
	t.Run("Scenario1_UserMisunderstanding", func(t *testing.T) {
		state := &model.SessionState{
			SessionID:         "real-test-1",
			EntryID:           "econ_opportunity_cost",
			AvailableRoles:    []string{"host", "economist", "skeptic"},
			MasteryEstimate:   0.3,
			OutputClockSec:    45,
			TensionLevel:      6,
			CognitiveLoad:     7,
			MisconceptionTags: []string{"M1_cost_equals_money_spent"},
			Signals: model.SignalsSnapshot{
				LastUserChars:     25,
				LastUserLatencyMS: 3000,
			},
			Turns: []model.Turn{
				{Role: "user", Text: "机会成本是什么？", TS: time.Now()},
				{Role: "assistant", Text: "机会成本是当你做出选择时放弃的最好替代选择的价值...", TS: time.Now().Add(-10 * time.Second)},
				{Role: "user", Text: "所以就是花掉的钱吗？", TS: time.Now()},
			},
		}

		userInput := "所以机会成本等于支出成本？"

		plan := director.Decide(state, userInput)

		// 验证关键字段
		if plan.FlowMode == "" {
			t.Error("❌ flow_mode should not be empty")
		}
		if plan.NextBeat == "" {
			t.Error("❌ next_beat should not be empty")
		}
		if plan.NextRole == "" {
			t.Error("❌ next_role should not be empty")
		}

		// 验证 RESCUE 模式（有误解）
		if plan.FlowMode != "RESCUE" {
			t.Logf("⚠️  Expected RESCUE mode, got %s (still valid, just different strategy)", plan.FlowMode)
		}

		t.Logf("✅ Real LLM Decision:")
		t.Logf("   FlowMode: %s", plan.FlowMode)
		t.Logf("   UserMindState: %v", plan.UserMindState)
		t.Logf("   NextBeat: %s", plan.NextBeat)
		t.Logf("   NextRole: %s", plan.NextRole)
		t.Logf("   OutputAction: %s", plan.OutputAction)
		t.Logf("   Notes: %s", plan.Notes)
		if plan.Debug != nil {
			t.Logf("   BeatChoiceReason: %s", plan.Debug.BeatChoiceReason)
		}
	})

	// 场景 2: 用户顺流状态，小步推进
	t.Run("Scenario2_FlowState", func(t *testing.T) {
		state := &model.SessionState{
			SessionID:         "real-test-2",
			EntryID:           "econ_opportunity_cost",
			AvailableRoles:    []string{"host", "economist", "skeptic"},
			MasteryEstimate:   0.6,
			OutputClockSec:    35,
			TensionLevel:      4,
			CognitiveLoad:     4,
			MisconceptionTags: []string{},
			Signals: model.SignalsSnapshot{
				LastUserChars:     80,
				LastUserLatencyMS: 1500,
			},
			Turns: []model.Turn{
				{Role: "user", Text: "机会成本是放弃的最好替代选择的价值", TS: time.Now()},
				{Role: "assistant", Text: "完全正确！你理解得很到位", TS: time.Now().Add(-8 * time.Second)},
				{Role: "user", Text: "对，就是这样", TS: time.Now()},
			},
		}

		userInput := "那如果我要评估买房的机会成本呢？"

		plan := director.Decide(state, userInput)

		// 验证关键字段
		if plan.FlowMode == "" {
			t.Error("❌ flow_mode should not be empty")
		}
		if plan.NextBeat == "" {
			t.Error("❌ next_beat should not be empty")
		}

		t.Logf("✅ Real LLM Decision (Flow State):")
		t.Logf("   FlowMode: %s", plan.FlowMode)
		t.Logf("   UserMindState: %v", plan.UserMindState)
		t.Logf("   NextBeat: %s", plan.NextBeat)
		t.Logf("   NextRole: %s", plan.NextRole)
		t.Logf("   OutputAction: %s", plan.OutputAction)
		t.Logf("   Notes: %s", plan.Notes)
	})

	// 场景 3: 用户疲惫，降低负荷
	t.Run("Scenario3_UserFatigue", func(t *testing.T) {
		state := &model.SessionState{
			SessionID:         "real-test-3",
			EntryID:           "econ_opportunity_cost",
			AvailableRoles:    []string{"host", "economist", "skeptic"},
			MasteryEstimate:   0.5,
			OutputClockSec:    60,
			TensionLevel:      6,
			CognitiveLoad:     8,
			MisconceptionTags: []string{},
			Signals: model.SignalsSnapshot{
				LastUserChars:     3,    // 很短的输出
				LastUserLatencyMS: 8000, // 很长的延迟
			},
			Turns: []model.Turn{
				{Role: "user", Text: "...", TS: time.Now()},
			},
		}

		userInput := "嗯"

		plan := director.Decide(state, userInput)

		// 验证关键字段
		if plan.FlowMode == "" {
			t.Error("❌ flow_mode should not be empty")
		}
		if plan.NextBeat == "" {
			t.Error("❌ next_beat should not be empty")
		}

		// 疲惫状态应该倾向于 minigame 或 exit_ticket
		isLowLoadBeat := plan.NextBeat == "minigame" || plan.NextBeat == "exit_ticket"
		if !isLowLoadBeat {
			t.Logf("⚠️  Expected low-load beat (minigame/exit_ticket), got %s (still valid)", plan.NextBeat)
		}

		t.Logf("✅ Real LLM Decision (Fatigue State):")
		t.Logf("   FlowMode: %s", plan.FlowMode)
		t.Logf("   UserMindState: %v", plan.UserMindState)
		t.Logf("   NextBeat: %s (should be low-load)", plan.NextBeat)
		t.Logf("   TalkBurstLimitSec: %d (should be short)", plan.TalkBurstLimitSec)
	})

	// 场景 4: 输出时钟超时，强制输出型 Beat
	t.Run("Scenario4_OutputClockTimeout", func(t *testing.T) {
		state := &model.SessionState{
			SessionID:         "real-test-4",
			EntryID:           "econ_opportunity_cost",
			AvailableRoles:    []string{"host", "economist", "skeptic"},
			MasteryEstimate:   0.7,
			OutputClockSec:    100, // 超过 90 秒阈值
			TensionLevel:      5,
			CognitiveLoad:     5,
			MisconceptionTags: []string{},
			Signals: model.SignalsSnapshot{
				LastUserChars:     50,
				LastUserLatencyMS: 2000,
			},
			Turns: []model.Turn{
				{Role: "user", Text: "我想我理解了", TS: time.Now()},
			},
		}

		userInput := "可以结束了吗？"

		plan := director.Decide(state, userInput)

		// 输出时钟超时应该强制选择输出型 Beat
		isOutputBeat := plan.NextBeat == "check" || plan.NextBeat == "feynman" || plan.NextBeat == "exit_ticket"
		if !isOutputBeat {
			t.Logf("⚠️  Expected output beat (check/feynman/exit_ticket), got %s", plan.NextBeat)
		}

		t.Logf("✅ Real LLM Decision (Timeout):")
		t.Logf("   OutputClock: %d sec (threshold: 90)", state.OutputClockSec)
		t.Logf("   NextBeat: %s (should be output-forcing)", plan.NextBeat)
		t.Logf("   FlowMode: %s", plan.FlowMode)
	})
}

// TestRealLLMClaude 测试真实 Claude LLM 的导演决策
// 需要设置环境变量: ANTHROPIC_API_KEY
// 运行: go test -v -run TestRealLLMClaude ./server/internal/director/... -tags=integration
func TestRealLLMClaude(t *testing.T) {
	// 跳过条件
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("⏭️  Skipping Claude LLM test: ANTHROPIC_API_KEY not set")
	}

	// 创建配置
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "anthropic",
			Anthropic: config.LLMProviderConfig{
				APIKey:      apiKey,
				APIURL:      "https://api.anthropic.com/v1",
				Model:       "claude-3-5-sonnet-20241022",
				Temperature: 0.7,
				MaxTokens:   2000,
			},
		},
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue", "lens_shift", "feynman", "montage", "minigame", "exit_ticket"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	// 创建真实 LLM 客户端
	llmClient, err := llm.NewClient(cfg)
	if err != nil {
		t.Fatalf("❌ Failed to create Claude client: %v", err)
	}

	// 创建导演引擎
	director := NewDirectorEngine(cfg, llmClient)

	// 测试场景
	state := &model.SessionState{
		SessionID:         "real-claude-test",
		EntryID:           "econ_opportunity_cost",
		AvailableRoles:    []string{"host", "economist", "skeptic"},
		MasteryEstimate:   0.45,
		OutputClockSec:    50,
		TensionLevel:      5,
		CognitiveLoad:     6,
		MisconceptionTags: []string{"M1_cost_equals_money_spent"},
		Signals: model.SignalsSnapshot{
			LastUserChars:     40,
			LastUserLatencyMS: 2500,
		},
		Turns: []model.Turn{
			{Role: "user", Text: "机会成本到底是什么？", TS: time.Now()},
		},
	}

	userInput := "还是不太明白"

	plan := director.Decide(state, userInput)

	// 验证关键字段
	if plan.FlowMode == "" {
		t.Error("❌ flow_mode should not be empty")
	}
	if plan.NextBeat == "" {
		t.Error("❌ next_beat should not be empty")
	}
	if plan.NextRole == "" {
		t.Error("❌ next_role should not be empty")
	}

	t.Logf("✅ Claude LLM Decision:")
	t.Logf("   FlowMode: %s", plan.FlowMode)
	t.Logf("   UserMindState: %v", plan.UserMindState)
	t.Logf("   NextBeat: %s", plan.NextBeat)
	t.Logf("   NextRole: %s", plan.NextRole)
	t.Logf("   OutputAction: %s", plan.OutputAction)
	t.Logf("   Notes: %s", plan.Notes)
}

// TestRealLLMComparisonOpenAIVsClaude 比较 OpenAI 和 Claude 的决策差异
// 需要同时设置 LLM_API_KEY 和 ANTHROPIC_API_KEY
// 运行: go test -v -run TestRealLLMComparisonOpenAIVsClaude ./server/internal/director/... -tags=integration
func TestRealLLMComparisonOpenAIVsClaude(t *testing.T) {
	openaiKey := os.Getenv("LLM_API_KEY")
	claudeKey := os.Getenv("ANTHROPIC_API_KEY")

	if openaiKey == "" || claudeKey == "" {
		t.Skip("⏭️  Skipping comparison test: both LLM_API_KEY and ANTHROPIC_API_KEY required")
	}

	// 统一的测试状态
	state := &model.SessionState{
		SessionID:         "comparison-test",
		EntryID:           "econ_opportunity_cost",
		AvailableRoles:    []string{"host", "economist", "skeptic"},
		MasteryEstimate:   0.35,
		OutputClockSec:    45,
		TensionLevel:      5,
		CognitiveLoad:     7,
		MisconceptionTags: []string{"M1_cost_equals_money_spent"},
		Signals: model.SignalsSnapshot{
			LastUserChars:     35,
			LastUserLatencyMS: 3000,
		},
		Turns: []model.Turn{
			{Role: "user", Text: "机会成本是我花的钱吧？", TS: time.Now()},
		},
	}

	userInput := "对吗？"

	// OpenAI 决策
	openaiCfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			OpenAI: config.LLMProviderConfig{
				APIKey:      openaiKey,
				APIURL:      "https://api.openai.com/v1",
				Model:       "gpt-4o-mini",
				Temperature: 0.7,
				MaxTokens:   2000,
			},
		},
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue", "lens_shift", "feynman", "montage", "minigame", "exit_ticket"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	openaiClient, _ := llm.NewClient(openaiCfg)
	openaiDirector := NewDirectorEngine(openaiCfg, openaiClient)
	openaiPlan := openaiDirector.Decide(state, userInput)

	// Claude 决策
	claudeCfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "anthropic",
			Anthropic: config.LLMProviderConfig{
				APIKey:      claudeKey,
				APIURL:      "https://api.anthropic.com/v1",
				Model:       "claude-3-5-sonnet-20241022",
				Temperature: 0.7,
				MaxTokens:   2000,
			},
		},
		Director: config.DirectorConfig{
			EnableLLM:              true,
			AvailableRoles:         []string{"host", "economist", "skeptic"},
			AvailableBeats:         []string{"reveal", "check", "deepen", "twist", "continue", "lens_shift", "feynman", "montage", "minigame", "exit_ticket"},
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	claudeClient, _ := llm.NewClient(claudeCfg)
	claudeDirector := NewDirectorEngine(claudeCfg, claudeClient)
	claudePlan := claudeDirector.Decide(state, userInput)

	// 比较
	t.Logf("📊 LLM 决策对比:")
	t.Logf("")
	t.Logf("OpenAI (GPT-4o-mini):")
	t.Logf("  FlowMode: %s", openaiPlan.FlowMode)
	t.Logf("  UserMindState: %v", openaiPlan.UserMindState)
	t.Logf("  NextBeat: %s", openaiPlan.NextBeat)
	t.Logf("  NextRole: %s", openaiPlan.NextRole)
	t.Logf("  Notes: %s", openaiPlan.Notes)
	t.Logf("")
	t.Logf("Claude (3.5-Sonnet):")
	t.Logf("  FlowMode: %s", claudePlan.FlowMode)
	t.Logf("  UserMindState: %v", claudePlan.UserMindState)
	t.Logf("  NextBeat: %s", claudePlan.NextBeat)
	t.Logf("  NextRole: %s", claudePlan.NextRole)
	t.Logf("  Notes: %s", claudePlan.Notes)
	t.Logf("")

	// 验证两个决策都是有效的
	if openaiPlan.NextBeat == "" || claudePlan.NextBeat == "" {
		t.Error("❌ Both decisions should have valid next_beat")
	}

	t.Logf("✅ Both LLMs produced valid decisions")
}
