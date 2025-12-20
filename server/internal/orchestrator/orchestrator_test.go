package orchestrator

import (
	"context"
	"testing"
	"time"

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
