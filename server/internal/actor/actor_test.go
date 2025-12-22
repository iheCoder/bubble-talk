package actor

import (
	"bubble-talk/server/internal/model"
	"strings"
	"testing"
)

func TestNewActorEngine(t *testing.T) {
	engine, err := NewActorEngine("../../configs/prompts")
	if err != nil {
		t.Fatalf("Failed to create actor engine: %v", err)
	}
	if len(engine.rolePrompts) == 0 {
		t.Error("Expected role prompts")
	}
	t.Logf("✓ 加载了 %d 个角色", len(engine.rolePrompts))
	for role := range engine.rolePrompts {
		t.Logf("  - 角色: %s", role)
	}
}

func TestBuildPrompt(t *testing.T) {
	engine, err := NewActorEngine("../../configs/prompts")
	if err != nil {
		t.Fatalf("Failed to create actor engine: %v", err)
	}

	req := ActorRequest{
		SessionID: "test-1",
		TurnID:    "turn-1",
		Plan: model.DirectorPlan{
			NextRole: "host",
			Instruction: "User Mind State: confused, fog\n" +
				"Next Beat: reveal\n" +
				"Output Action: explain_with_metaphor\n" +
				"Tension Goal: decrease\n" +
				"Load Goal: decrease\n",
		},
		MainObjective: "理解机会成本",
		ConceptName:   "机会成本",
		LastUserText:  "我不太明白",
		Metaphor:      "咖啡店选择",
	}

	prompt, err := engine.BuildPrompt(req)
	if err != nil {
		t.Fatalf("Failed to build prompt: %v", err)
	}

	// 验证必要部分
	requiredSections := []string{
		"[Role Definition]",
		"[Context]",
		"[Director Instructions]",
		"[Constraints]",
	}
	for _, section := range requiredSections {
		if !strings.Contains(prompt.Instructions, section) {
			t.Errorf("Missing section: %s", section)
		}
	}

	if prompt.DebugInfo["role"] != "host" {
		t.Error("Expected role in debug info")
	}

	t.Log("✓ 成功构建Prompt")
	t.Logf("\n=== Prompt ===\n%s\n============\n", prompt.Instructions)
}

func TestValidate(t *testing.T) {
	engine, _ := NewActorEngine("../../configs/prompts")

	t.Run("有效Prompt", func(t *testing.T) {
		req := ActorRequest{
			SessionID: "test-2",
			Plan: model.DirectorPlan{
				NextRole:    "host",
				Instruction: "Next Beat: reveal\nTalk Burst Limit: 20 seconds\n",
			},
			MainObjective: "测试",
		}
		prompt, _ := engine.BuildPrompt(req)
		if err := engine.Validate(prompt); err != nil {
			t.Errorf("Expected valid: %v", err)
		}
		t.Log("✓ 校验通过")
	})

	t.Run("空Prompt", func(t *testing.T) {
		prompt := ActorPrompt{Instructions: ""}
		if err := engine.Validate(prompt); err == nil {
			t.Error("Expected error for empty")
		}
		t.Log("✓ 正确拒绝空Prompt")
	})

	t.Run("过长Prompt", func(t *testing.T) {
		prompt := ActorPrompt{Instructions: strings.Repeat("x", 3000)}
		if err := engine.Validate(prompt); err == nil {
			t.Error("Expected error for too long")
		}
		t.Log("✓ 正确拒绝过长Prompt")
	})
}

func TestBuildFallbackPrompt(t *testing.T) {
	engine, _ := NewActorEngine("../../configs/prompts")

	req := ActorRequest{
		SessionID:     "test-3",
		Plan:          model.DirectorPlan{NextRole: "host", Instruction: "Next Beat: check\n"},
		MainObjective: "理解机会成本",
	}

	prompt := engine.BuildFallbackPrompt(req)

	if !strings.Contains(prompt.Instructions, "helpful tutor") {
		t.Error("Expected helpful tutor")
	}
	if !strings.Contains(prompt.Instructions, req.MainObjective) {
		t.Error("Expected main objective")
	}
	if fallback, ok := prompt.DebugInfo["fallback"].(bool); !ok || !fallback {
		t.Error("Expected fallback flag")
	}

	t.Log("✓ 成功构建兜底Prompt")
	t.Logf("\n=== Fallback ===\n%s\n===============\n", prompt.Instructions)
}

func TestFullWorkflow(t *testing.T) {
	engine, err := NewActorEngine("../../configs/prompts")
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	req := ActorRequest{
		SessionID: "integration-1",
		TurnID:    "turn-1",
		Plan: model.DirectorPlan{
			NextRole: "host",
			Instruction: "User Mind State: confused, fog\n" +
				"Intent: clarify\n" +
				"Next Beat: reveal\n" +
				"Output Action: explain_with_metaphor\n" +
				"Tension Goal: decrease\n" +
				"Load Goal: decrease\n" +
				"Notes: 用户对机会成本感到困惑\n",
		},
		EntryID:       "entry-econ-001",
		Domain:        "economics",
		MainObjective: "理解机会成本的定义和应用",
		ConceptName:   "机会成本",
		LastUserText:  "我不明白为什么要考虑没选的那个选项",
		Metaphor:      "去咖啡店点饮料",
	}

	// 1. 构建
	prompt, err := engine.BuildPrompt(req)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// 2. 校验
	if err := engine.Validate(prompt); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// 3. 验证内容
	requiredContent := []string{"confused", "reveal", "metaphor"}
	for _, content := range requiredContent {
		if !strings.Contains(strings.ToLower(prompt.Instructions), strings.ToLower(content)) {
			t.Errorf("Missing content: %s", content)
		}
	}

	t.Log("\n" + strings.Repeat("=", 80))
	t.Log("完整Prompt示例 (Host + Reveal)")
	t.Log(strings.Repeat("=", 80))
	t.Log(prompt.Instructions)
	t.Log(strings.Repeat("=", 80))

	t.Log("\n✓ 完整工作流测试通过")
}
