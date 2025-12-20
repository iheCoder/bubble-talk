package timeline

import (
	"context"
	"testing"

	"bubble-talk/server/internal/model"
)

// TestInMemoryStoreAppendAssignsSeq 验证 Append 方法为事件分配正确的 seq。
// 场景：连续追加两个事件，验证 seq 递增。
func TestInMemoryStoreAppendAssignsSeq(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	seq1, err := store.Append(ctx, "s1", &model.Event{Type: "user_message"})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	if seq1 != 1 {
		t.Fatalf("expected seq 1, got %d", seq1)
	}

	seq2, err := store.Append(ctx, "s1", &model.Event{Type: "user_message"})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	if seq2 != 2 {
		t.Fatalf("expected seq 2, got %d", seq2)
	}
}

// TestInMemoryStoreAppendIdempotentByEventID 验证 Append 方法对相同 EventID 的幂等性。
// 场景：追加两个具有相同 EventID 的事件，验证返回的 seq 相同且只存储一个事件。
func TestInMemoryStoreAppendIdempotentByEventID(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	seq1, err := store.Append(ctx, "s1", &model.Event{Type: "user_message", EventID: "evt-1"})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	seq2, err := store.Append(ctx, "s1", &model.Event{Type: "user_message", EventID: "evt-1"})
	if err != nil {
		t.Fatalf("append duplicate event: %v", err)
	}
	if seq2 != seq1 {
		t.Fatalf("expected same seq for duplicate event_id, got %d vs %d", seq1, seq2)
	}

	events, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event stored, got %d", len(events))
	}
}

// TestInMemoryStoreListReturnsCopy 验证 List 方法返回事件切片的副本，防止外部修改影响内部状态。
// 场景：修改返回的事件切片，验证内部存储未受影响。
func TestInMemoryStoreListReturnsCopy(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	if _, err := store.Append(ctx, "s1", &model.Event{Type: "user_message", Text: "hi"}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	events, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	events[0].Type = "mutated"

	eventsAgain, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatalf("list events again: %v", err)
	}
	if eventsAgain[0].Type != "user_message" {
		t.Fatalf("expected internal data unchanged, got %q", eventsAgain[0].Type)
	}
}
