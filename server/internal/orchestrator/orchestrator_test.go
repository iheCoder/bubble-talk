package orchestrator

import (
	"bubble-talk/server/internal/llm"
	"context"
	"log"
	"os"
	"testing"
	"time"

	"bubble-talk/server/internal/actor"
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/director"
	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"
)

// TestOrchestratorOnEventAppendsTimelineAndUpdatesSnapshot 验证 Orchestrator.OnEvent 的核心功能。
// 场景：收到用户消息事件，期望在 timeline 中追加用户消息、导演计划、助手回复三条事件，且更新 session 快照。
func TestOrchestratorOnEventAppendsTimelineAndUpdatesSnapshot(t *testing.T) {
	store := session.NewInMemoryStore()
	timelineStore := timeline.NewInMemoryStore()
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	orch := New(store, timelineStore, func() time.Time { return now })
	state := &model.SessionState{
		SessionID:     "s1",
		EntryID:       "entry",
		MainObjective: "objective",
		LastOutputAt:  now.Add(-30 * time.Second),
	}
	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("save session: %v", err)
	}

	resp, err := orch.OnEvent(context.Background(), "s1", model.Event{Type: "user_message", Text: "hi"})
	if err != nil {
		t.Fatalf("on event: %v", err)
	}
	if resp == nil || resp.Assistant.Text == "" {
		t.Fatalf("expected assistant response")
	}

	events, err := timelineStore.List(context.Background(), "s1")
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events appended, got %d", len(events))
	}
	if events[0].Type != "user_message" || events[1].Type != "director_plan" || events[2].Type != "assistant_text" {
		t.Fatalf("unexpected event order: %s, %s, %s", events[0].Type, events[1].Type, events[2].Type)
	}

	updated, err := store.Get(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(updated.Turns) != 2 {
		t.Fatalf("expected 2 turns recorded, got %d", len(updated.Turns))
	}
	if updated.OutputClockSec != 0 {
		t.Fatalf("expected OutputClockSec reset to 0, got %d", updated.OutputClockSec)
	}
}

// TestHandleUserUtteranceIntegration 验证 HandleUserUtterance 的端到端行为。
// 场景：用户输入后应写入 Timeline，生成 director_plan，并更新 Session 快照。
func TestHandleUserUtteranceIntegration(t *testing.T) {
	// 跳过条件：没有 API Key 或没有指定 -tags=integration
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Skip("⏭️  Skipping real LLM test: LLM_API_KEY not set")
	}

	store := session.NewInMemoryStore()
	timelineStore := timeline.NewInMemoryStore()
	now := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: "openai",
			OpenAI: config.LLMProviderConfig{
				APIKey:      apiKey,
				APIURL:      "https://api.openai.com/v1",
				Model:       "gpt-5-nano-2025-08-07", // 使用便宜的模型进行测试
				Temperature: 1,
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

	directorEngine := director.NewDirectorEngine(cfg, llmClient)
	actorEngine, err := actor.NewActorEngine("../../configs/prompts")
	if err != nil {
		t.Fatalf("create actor engine: %v", err)
	}

	orch := NewWithEngines(store, timelineStore, directorEngine, actorEngine, log.Default())

	state := &model.SessionState{
		SessionID:     "s1",
		EntryID:       "entry",
		MainObjective: "objective",
		LastOutputAt:  now.Add(-30 * time.Second),
	}
	if err := store.Save(context.Background(), state); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if err := orch.HandleUserUtterance(context.Background(), "s1", "hello", nil); err != nil {
		t.Fatalf("handle user utterance: %v", err)
	}

	events, err := timelineStore.List(context.Background(), "s1")
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events appended, got %d", len(events))
	}
	if events[0].Type != "user_utterance" || events[1].Type != "director_plan" {
		t.Fatalf("unexpected event order: %s, %s", events[0].Type, events[1].Type)
	}

	updated, err := store.Get(context.Background(), "s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.LastUserUtterance != "hello" {
		t.Fatalf("expected last user utterance to be saved")
	}
	if len(updated.Turns) != 1 {
		t.Fatalf("expected 1 turn recorded, got %d", len(updated.Turns))
	}
	if updated.UpdatedAt.Before(now) {
		t.Fatalf("expected UpdatedAt to be refreshed")
	}
}
