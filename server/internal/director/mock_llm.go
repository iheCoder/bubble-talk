package director

import (
	"bubble-talk/server/internal/llm"
	"context"
	"encoding/json"
)

// MockLLMClient 用于测试的 Mock LLM 客户端
type MockLLMClient struct {
	// 控制 LLM 的行为
	ResponsePlan *struct {
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
	}
	ShouldFail bool
	CallCount  int
}

// NewMockLLMClient 创建 Mock LLM 客户端
func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		CallCount: 0,
	}
}

// Complete 模拟 LLM Complete 方法
func (m *MockLLMClient) Complete(ctx context.Context, messages []llm.Message, schema *llm.JSONSchema) (string, error) {
	m.CallCount++

	if m.ShouldFail {
		return "", context.DeadlineExceeded
	}

	// 返回默认的决策计划（如果没有指定）
	if m.ResponsePlan == nil {
		m.ResponsePlan = &struct {
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
			FlowMode:          "FLOW",
			UserMindState:     []string{"Engaged"},
			Intent:            "continue",
			NextBeat:          "check",
			NextRole:          "host",
			OutputAction:      "ask_simple_question",
			TalkBurstLimitSec: 20,
			Notes:             "LLM decision for testing",
		}
	}

	// 序列化为 JSON
	data, _ := json.Marshal(m.ResponsePlan)
	return string(data), nil
}

// SetResponsePlan 设置 LLM 要返回的决策计划
func (m *MockLLMClient) SetResponsePlan(plan interface{}) {
	data, _ := json.Marshal(plan)
	json.Unmarshal(data, &m.ResponsePlan)
}
