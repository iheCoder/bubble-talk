package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bubble-talk/server/internal/config"
)

func TestRealLLMTalOpenAI_ClaudeStyleJSON(t *testing.T) {
	// Prepare a server that returns a raw JSON object (Claude-style)
	respBody := `{
  "flow_mode": "RESCUE",
  "user_mind_state": ["Fog"],
  "next_beat": "reveal",
  "next_role": "economist",
  "user_output_requirement": "用自己的话复述：机会成本和花的钱有什么区别",
  "tension_goal": "maintain",
  "load_goal": "decrease",
  "reasoning": "用户明确说'还是不太明白'，加上存在M1误解（把成本等同于花的钱）和较高认知负荷(6)，处于迷雾状态需要救场。选择reveal用简单比喻重新解释，由economist角色用专业但易懂的方式降维打击。负荷需要降低，张力维持中等以保持注意力。"
}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody))
	}))
	defer ts.Close()

	providerCfg := config.LLMProviderConfig{
		APIURL: ts.URL,
		APIKey: "dummy",
		Model:  "claude-opus-4.5",
	}

	client := NewTalOpenAIClient(providerCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := []Message{{Role: "user", Content: "test"}}

	res, err := client.Complete(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}

	// Expect returned string to be compact JSON containing keys like "flow_mode"
	if res == "" {
		t.Fatalf("expected non-empty response")
	}
	if !contains(res, "flow_mode") || !contains(res, "next_beat") {
		t.Fatalf("unexpected response content: %s", res)
	}
}

func TestRealLLMTalOpenAI_OpenAIStyle(t *testing.T) {
	// Simulate OpenAI-style response
	respBody := `{"choices":[{"message":{"content":"hello world"}}]}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody))
	}))
	defer ts.Close()

	providerCfg := config.LLMProviderConfig{
		APIURL: ts.URL,
		APIKey: "dummy",
		Model:  "gpt-test",
	}

	client := NewTalOpenAIClient(providerCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := []Message{{Role: "user", Content: "test"}}

	res, err := client.Complete(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}

	if res != "hello world" {
		t.Fatalf("unexpected response: %s", res)
	}
}

func TestRealLLMTalOpenAI_OpenAIContentWithJSON(t *testing.T) {
	// Simulate OpenAI-style response where content is a multi-line JSON object
	contentJSON := `{
  "flow_mode": "RESCUE",
  "user_mind_state": ["Fog"],
  "next_beat": "reveal",
  "next_role": "economist",
  "user_output_requirement": "用一句话复述：机会成本是什么",
  "tension_goal": "maintain",
  "load_goal": "decrease",
  "reasoning": "用户明确表示'还是不太明白'，存在M1误解（把成本等同为花掉的钱），掌握度0.45偏低，认知负荷6较高——典型的Fog状态需要救场。选择reveal用简单比喻降维解释，由economist角色用专业但通俗的方式重新讲解机会成本的真正含义。负荷已偏高需降低，张力5适中保持。"
}`

	respBody := `{"choices":[{"message":{"content":` + string(jsonEscape(contentJSON)) + `}}]}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody))
	}))
	defer ts.Close()

	providerCfg := config.LLMProviderConfig{
		APIURL: ts.URL,
		APIKey: "dummy",
		Model:  "gpt-test",
	}

	client := NewTalOpenAIClient(providerCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := []Message{{Role: "user", Content: "test"}}

	res, err := client.Complete(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}

	// Returned string should be valid JSON that can be unmarshaled
	var m map[string]any
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		t.Fatalf("returned content is not valid JSON: %v\ncontent: %s", err, res)
	}

	// Check some keys
	if _, ok := m["flow_mode"]; !ok {
		t.Fatalf("expected flow_mode key in returned JSON: %s", res)
	}
	if _, ok := m["next_beat"]; !ok {
		t.Fatalf("expected next_beat key in returned JSON: %s", res)
	}
}

// jsonEscape returns a JSON-quoted string of s (i.e., suitable to embed inside JSON)
func jsonEscape(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && ( // quick contains without importing strings for minimalism
	func() bool { return stringsIndex(s, sub) >= 0 }())
}

// simple implementation of strings.Index to avoid adding import
func stringsIndex(s, sep string) int {
	if sep == "" {
		return 0
	}
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
