package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/llm"
	"bubble-talk/server/internal/model"
	"context"
	"strings"
	"testing"
	"time"
)

// MockLLMClient 模拟 LLM 客户端
type MockLLMClient struct {
	response string
	err      error
}

func (m *MockLLMClient) Complete(ctx context.Context, messages []llm.Message, schema *llm.JSONSchema) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// TestSegmentDirector_DecideSegment_NoUserInput 测试无用户输入时的剧情推进
func TestSegmentDirector_DecideSegment_NoUserInput(t *testing.T) {
	// 准备测试数据
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM: true,
		},
	}

	// Mock LLM 返回
	mockLLM := &MockLLMClient{
		response: `{
			"role_id": "host",
			"scene_direction": "主持人用轻松的口吻抛出问题：你周末加班赚了800块，朋友喊你去看演唱会你没去。你觉得你是赚了还是亏了？制造认知冲突，让用户思考。说完后停下来，观察用户反应。",
			"narrative_mode": "INTERVIEW",
			"narrative_tone": "LIGHT",
			"teaching_goal": "引发对机会成本的思考",
			"user_must_do_type": "choice",
			"user_must_do_prompt": "你觉得是赚了还是亏了？",
			"max_duration_sec": 45,
			"director_notes": "开场制造冲突，引发好奇"
		}`,
	}

	director := NewSegmentDirector(cfg, mockLLM)

	// 创建会话状态
	state := &model.SessionState{
		SessionID:       "test_session",
		EntryID:         "test_entry",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.3,
		CognitiveLoad:   5,
		TensionLevel:    4,
		Signals: model.SignalsSnapshot{
			LastUserChars:     0,
			LastUserLatencyMS: 0,
		},
		Turns: []model.Turn{},
	}

	ctx := context.Background()

	// 执行决策
	segmentPlan, err := director.DecideSegment(ctx, state, "")

	if err != nil {
		t.Fatalf("DecideSegment failed: %v", err)
	}

	// 验证结果
	if segmentPlan.RoleID != "host" {
		t.Errorf("Expected role_id 'host', got '%s'", segmentPlan.RoleID)
	}

	if segmentPlan.SceneDirection == "" {
		t.Error("SceneDirection should not be empty")
	}

	if len(segmentPlan.SceneDirection) < 50 {
		t.Errorf("SceneDirection too short: %d characters", len(segmentPlan.SceneDirection))
	}

	t.Logf("✅ Segment generated successfully:")
	t.Logf("   Role: %s", segmentPlan.RoleID)
	t.Logf("   Scene: %s", segmentPlan.SceneDirection[:50]+"...")
}

// TestSegmentDirector_DecideSegment_WithUserInput 测试有用户输入时的回应
func TestSegmentDirector_DecideSegment_WithUserInput(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM: true,
		},
	}

	mockLLM := &MockLLMClient{
		response: `{
			"role_id": "host",
			"scene_direction": "主持人先肯定用户的观察：你说得对，800块确实是收入。但我想请你再想一个问题：那场演唱会呢？你放弃的那个选择，值多少？不要急着给答案，停下来让用户思考。",
			"user_intent": "confused",
			"user_mind_state": ["Partial"],
			"response_approach": "先肯定用户的直觉，再用反问引导用户思考机会成本",
			"need_user_output": true,
			"narrative_mode": "INTERVIEW",
			"narrative_tone": "LIGHT",
			"teaching_goal": "引导用户理解机会成本不是花了多少",
			"user_must_do_type": "teach_back",
			"user_must_do_prompt": "你觉得那场演唱会值多少？",
			"max_duration_sec": 60,
			"director_notes": "用户有困惑，需要温和引导"
		}`,
	}

	director := NewSegmentDirector(cfg, mockLLM)

	state := &model.SessionState{
		SessionID:       "test_session",
		EntryID:         "test_entry",
		AvailableRoles:  []string{"host", "economist"},
		MasteryEstimate: 0.4,
		CognitiveLoad:   6,
		TensionLevel:    5,
		Signals: model.SignalsSnapshot{
			LastUserChars:     25,
			LastUserLatencyMS: 2000,
		},
		Turns: []model.Turn{
			{Role: "user", Text: "我觉得赚了啊，800块到手了", TS: time.Now()},
		},
	}

	ctx := context.Background()

	segmentPlan, err := director.DecideSegment(ctx, state, "我觉得赚了啊，800块到手了")

	if err != nil {
		t.Fatalf("DecideSegment failed: %v", err)
	}

	// 验证回应策略存在
	if segmentPlan.UserResponseStrategy == nil {
		t.Error("UserResponseStrategy should not be nil when there's user input")
	}

	if segmentPlan.UserResponseStrategy != nil {
		if segmentPlan.UserResponseStrategy.UserIntent == "" {
			t.Error("UserIntent should not be empty")
		}
		if segmentPlan.UserResponseStrategy.ResponseApproach == "" {
			t.Error("ResponseApproach should not be empty")
		}
	}

	t.Logf("✅ Response generated successfully:")
	t.Logf("   Intent: %s", segmentPlan.UserResponseStrategy.UserIntent)
	t.Logf("   Approach: %s", segmentPlan.UserResponseStrategy.ResponseApproach[:50]+"...")
}

// TestSegmentDirector_Continuity 测试剧情连贯性
func TestSegmentDirector_Continuity(t *testing.T) {
	// TODO: 测试上一段和下一段的连贯性
	// 验证：如果上一段由 host 结束且在等待，下一段应该继续用 host 或有合理过渡
	t.Skip("Continuity test to be implemented")
}

// TestSegmentDirector_RoleInteraction 测试角色间互动
func TestSegmentDirector_RoleInteraction(t *testing.T) {
	// TODO: 测试角色间对话（不回应用户，而是角色互动推进剧情）
	t.Skip("Role interaction test to be implemented")
}

// TestSegmentDirector_StoryProgress 测试故事摘要
func TestSegmentDirector_StoryProgress(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{},
	}

	mockLLM := &MockLLMClient{
		response: `【剧情进展】：主持人抛出了"周末加班赚800"的例子，用户初步回应认为"赚了"
【用户参与】：用户展现出直觉思维，还没意识到机会成本
【当前状态】：冲突已制造，待引导用户思考"放弃了什么"
【待解决】：机会成本的核心概念还未澄清`,
	}

	director := NewSegmentDirector(cfg, mockLLM)

	state := &model.SessionState{
		MasteryEstimate:   0.3,
		MisconceptionTags: []string{"M1_cost_equals_money_spent"},
		CognitiveLoad:     5,
		Turns: []model.Turn{
			{Role: "host", Text: "你周末加班赚了800块，朋友喊你去看演唱会你没去。你觉得你是赚了还是亏了？", TS: time.Now()},
			{Role: "user", Text: "我觉得赚了啊，800块到手了", TS: time.Now()},
		},
	}

	ctx := context.Background()

	progress := director.summarizeStoryProgress(ctx, state)

	if progress == "" {
		t.Error("Story progress should not be empty")
	}

	if len(progress) < 100 {
		t.Errorf("Story progress too short: %d characters", len(progress))
	}

	// 验证包含关键部分
	if !strings.Contains(progress, "剧情进展") {
		t.Error("Story progress should contain '剧情进展'")
	}

	t.Logf("✅ Story progress generated:")
	t.Logf("%s", progress)
}
