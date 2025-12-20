package orchestrator

import (
	"testing"
	"time"

	"bubble-talk/server/internal/model"
)

// TestReduceUserMessageUpdatesSignalsAndClock 验证用户消息事件正确更新信号和输出时钟。
// 场景：用户发送消息后，输出时钟应反映自上次输出以来的时间，信号应记录用户输入字符数，且用户发言应被记录为一个对话轮次。
func TestReduceUserMessageUpdatesSignalsAndClock(t *testing.T) {
	state := &model.SessionState{
		SessionID:    "s1",
		LastOutputAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	now := time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC)

	evt := model.Event{Type: "user_message", Text: "hello"}
	Reduce(state, evt, now)

	if state.OutputClockSec != 60 {
		t.Fatalf("expected OutputClockSec 60, got %d", state.OutputClockSec)
	}
	if state.Signals.LastUserChars != len(evt.Text) {
		t.Fatalf("expected LastUserChars %d, got %d", len(evt.Text), state.Signals.LastUserChars)
	}
	if len(state.Turns) != 1 || state.Turns[0].Role != "user" {
		t.Fatalf("expected one user turn recorded")
	}
}

// TestReduceAssistantTextResetsClock 验证助手文本事件正确重置输出时钟。
// 场景：助手发送文本后，输出时钟应重置为0，最近输出时间应更新，且助手发言应被记录为一个对话轮次。
func TestReduceAssistantTextResetsClock(t *testing.T) {
	state := &model.SessionState{
		SessionID:      "s1",
		OutputClockSec: 42,
	}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	evt := model.Event{Type: "assistant_text", Text: "ok"}
	Reduce(state, evt, now)

	if state.OutputClockSec != 0 {
		t.Fatalf("expected OutputClockSec reset to 0, got %d", state.OutputClockSec)
	}
	if state.LastOutputAt.IsZero() {
		t.Fatalf("expected LastOutputAt updated")
	}
	if len(state.Turns) != 1 || state.Turns[0].Role != "assistant" {
		t.Fatalf("expected one assistant turn recorded")
	}
}
