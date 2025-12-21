package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/model"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestSegmentDirector_E2E_简化版 端到端测试
func TestSegmentDirector_E2E_Simplified(t *testing.T) {
	// 准备配置
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM: true,
		},
	}

	// Mock LLM 返回简化的 SegmentPlan
	mockLLM := &SegmentTestLLMClient{
		Responses: map[string]string{
			"segment_plan": `{
			"role_id": "host",
			"scene_direction": "主持人用轻松的口吻，抛出一个反直觉的例子：你周末加班赚了800块，朋友喊你去看演唱会你没去。你觉得你是赚了还是亏了？制造认知冲突，让用户思考。说完后停下来，等用户反应。",
			"max_duration_sec": 45,
			"director_notes": "开场制造冲突，引发好奇"
		}`,
		},
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

	// 验证简化后的 SegmentPlan
	if segmentPlan.RoleID != "host" {
		t.Errorf("Expected role_id 'host', got '%s'", segmentPlan.RoleID)
	}

	if segmentPlan.SceneDirection == "" {
		t.Error("SceneDirection should not be empty")
	}

	if segmentPlan.MaxDurationSec != 45 {
		t.Errorf("Expected max_duration_sec 45, got %d", segmentPlan.MaxDurationSec)
	}

	// 验证不再有复杂字段
	// （通过反射检查，确保没有 NarrativeTilt 等字段）
	planJSON, _ := json.Marshal(segmentPlan)
	planStr := string(planJSON)

	if strings.Contains(planStr, "narrative_tilt") {
		t.Error("SegmentPlan should not contain narrative_tilt")
	}
	if strings.Contains(planStr, "segment_goal") {
		t.Error("SegmentPlan should not contain segment_goal")
	}
	if strings.Contains(planStr, "autonomy_budget") {
		t.Error("SegmentPlan should not contain autonomy_budget")
	}

	t.Logf("✅ 简化的 SegmentPlan 验证通过:")
	t.Logf("   Role: %s", segmentPlan.RoleID)
	t.Logf("   Scene: %s", segmentPlan.SceneDirection[:50]+"...")
	t.Logf("   MaxDuration: %d秒", segmentPlan.MaxDurationSec)
}

// TestSegmentRunner_Execute 测试片段执行
func TestSegmentRunner_Execute(t *testing.T) {
	runner := NewSegmentRunner()

	// 创建一个简单的 SegmentPlan
	plan := &model.SegmentPlan{
		SegmentID:      "seg_test_001",
		RoleID:         "host",
		SceneDirection: "主持人抛出问题，然后等用户反应",
		MaxDurationSec: 30,
		DirectorNotes:  "测试用",
	}

	state := &model.SessionState{
		SessionID:      "test_session",
		AvailableRoles: []string{"host"},
		Turns:          []model.Turn{},
	}

	ctx := context.Background()

	// 执行 Segment
	turns, err := runner.RunSegment(ctx, plan, state)
	if err != nil {
		t.Fatalf("RunSegment failed: %v", err)
	}

	// 验证结果
	if len(turns) == 0 {
		t.Error("Expected at least one turn")
	}

	if state.CurrentSegment == nil {
		t.Error("CurrentSegment should be set")
	}

	if state.CurrentSegment.Status != "COMPLETED" {
		t.Errorf("Expected status COMPLETED, got %s", state.CurrentSegment.Status)
	}

	t.Logf("✅ Segment 执行成功:")
	t.Logf("   生成轮次: %d", len(turns))
	t.Logf("   用时: %d秒", state.CurrentSegment.ElapsedSec)
}

// TestDirector_StoryProgressConditional 测试故事摘要条件化
func TestDirector_StoryProgressConditional(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{},
	}

	director := NewSegmentDirector(cfg, &MockLLMClient{})

	// 测试 1: 少于 5 轮对话，不应该生成摘要
	stateShort := &model.SessionState{
		Turns: []model.Turn{
			{Role: "host", Text: "你好", TS: time.Now()},
			{Role: "user", Text: "你好", TS: time.Now()},
		},
	}

	promptShort := director.buildSegmentUserPromptV2(nil, stateShort, "", "ADAPT", "")
	if strings.Contains(promptShort, "已发生的故事") {
		t.Error("对话少于5轮时，不应该包含故事摘要部分")
	}

	// 测试 2: 多于 5 轮对话，应该生成摘要
	stateLong := &model.SessionState{
		Turns: []model.Turn{
			{Role: "host", Text: "1", TS: time.Now()},
			{Role: "user", Text: "2", TS: time.Now()},
			{Role: "host", Text: "3", TS: time.Now()},
			{Role: "user", Text: "4", TS: time.Now()},
			{Role: "host", Text: "5", TS: time.Now()},
			{Role: "user", Text: "6", TS: time.Now()},
		},
	}

	promptLong := director.buildSegmentUserPromptV2(nil, stateLong, "", "ADAPT", "故事摘要")
	if !strings.Contains(promptLong, "已发生的故事") {
		t.Error("对话多于5轮时，应该包含故事摘要部分")
	}

	t.Log("✅ 故事摘要条件化测试通过")
}

// TestDirector_RoleDescriptionDynamic 测试角色描述动态拼接
func TestDirector_RoleDescriptionDynamic(t *testing.T) {
	cfg := &config.Config{
		Director: config.DirectorConfig{},
	}

	mockLLM := &MockLLMClient{}
	director := NewSegmentDirector(cfg, mockLLM)

	state := &model.SessionState{
		AvailableRoles: []string{"host", "economist", "custom_role"},
	}

	// 测试动态角色描述
	hostDesc := director.getRoleDescriptionFromState("host", state)
	if hostDesc == "" {
		t.Error("应该返回 host 的描述")
	}

	customDesc := director.getRoleDescriptionFromState("custom_role", state)
	if customDesc == "" {
		t.Error("自定义角色应该返回默认描述")
	}

	// 测试提示词中包含动态角色
	prompt := director.buildSegmentUserPromptV2(nil, state, "", "ADAPT", "")

	if !strings.Contains(prompt, "host") {
		t.Error("提示词应该包含 host 角色")
	}
	if !strings.Contains(prompt, "economist") {
		t.Error("提示词应该包含 economist 角色")
	}
	if !strings.Contains(prompt, "custom_role") {
		t.Error("提示词应该包含 custom_role 角色")
	}

	t.Log("✅ 角色描述动态拼接测试通过")
}

// TestDirector_BeatStrategy 测试 beat 策略提示
func TestDirector_BeatStrategy(t *testing.T) {
	cfg := &config.Config{}
	mockLLM := &MockLLMClient{}
	director := NewSegmentDirector(cfg, mockLLM)

	state := &model.SessionState{
		AvailableRoles: []string{"host"},
		Turns:          []model.Turn{},
	}

	// 有用户输入时，应该包含 beat 策略提示
	promptWithUser := director.buildSegmentUserPromptV2(nil, state, "我不太懂", "ADAPT", "")

	if !strings.Contains(promptWithUser, "beat 策略") {
		t.Error("有用户输入时，应该包含 beat 策略提示")
	}
	if !strings.Contains(promptWithUser, "困惑(Fog)") {
		t.Error("应该包含心理状态对应的 beat 策略")
	}

	// 无用户输入时，不应该包含 beat 策略
	promptNoUser := director.buildSegmentUserPromptV2(nil, state, "", "ADAPT", "")

	if strings.Contains(promptNoUser, "beat 策略") {
		t.Error("无用户输入时，不应该强调 beat 策略")
	}

	t.Log("✅ beat 策略提示测试通过")
}

// Test完整流程：Director -> Runner
func TestDirectorToRunner_FullFlow(t *testing.T) {
	// 1. 创建导演
	cfg := &config.Config{
		Director: config.DirectorConfig{},
	}
	mockLLM := &SegmentTestLLMClient{
		Responses: map[string]string{
			"segment_plan": `{
			"role_id": "host",
			"scene_direction": "主持人提出问题，等用户回答",
			"max_duration_sec": 30,
			"director_notes": "测试完整流程"
		}`,
		},
	}
	director := NewSegmentDirector(cfg, mockLLM)

	// 2. 创建执行器
	runner := NewSegmentRunner()

	// 3. 准备状态
	state := &model.SessionState{
		SessionID:      "flow_test",
		EntryID:        "test_entry",
		AvailableRoles: []string{"host"},
		Turns:          []model.Turn{},
	}

	ctx := context.Background()

	// 4. 导演决策
	plan, err := director.DecideSegment(ctx, state, "")
	if err != nil {
		t.Fatalf("决策失败: %v", err)
	}

	// 5. 执行器执行
	turns, err := runner.RunSegment(ctx, plan, state)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}

	// 6. 验证结果
	if len(turns) == 0 {
		t.Error("应该生成至少一轮对话")
	}

	if len(state.Turns) == 0 {
		t.Error("状态应该包含对话历史")
	}

	t.Logf("✅ 完整流程测试通过:")
	t.Logf("   导演决策 -> Segment: %s", plan.SegmentID)
	t.Logf("   执行器生成 -> %d 轮对话", len(turns))
	t.Logf("   状态更新 -> CurrentSegment: %s", state.CurrentSegment.SegmentID)
}
