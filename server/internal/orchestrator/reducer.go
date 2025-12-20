package orchestrator

import (
	"time"

	"bubble-talk/server/internal/model"
)

// Reduce 只做“事实归约”，不触发外部调用。
// 约定：state 来自快照缓存，任何输出/计划都应该通过事件回放重建。
func Reduce(state *model.SessionState, evt model.Event, now time.Time) *model.SessionState {
	if state == nil {
		return nil
	}

	switch evt.Type {
	case "assistant_text":
		// 输出类事件会重置 OutputClock，并更新最近输出时间。
		if evt.Text != "" {
			state.Turns = append(state.Turns, model.Turn{
				Role: "assistant",
				Text: evt.Text,
				TS:   now,
			})
			state.OutputClockSec = 0
			state.LastOutputAt = now
		}
	case "quiz_answer":
		// 工具答题也属于用户事实输入，计入 turns，但不改 OutputClock。
		if evt.Answer != "" {
			state.Turns = append(state.Turns, model.Turn{
				Role: "user",
				Text: evt.Answer,
				TS:   now,
			})
		}
	default:
		// 默认将文本类输入视为用户发言，更新信号与时钟。
		if evt.Text != "" {
			if !state.LastOutputAt.IsZero() {
				state.OutputClockSec = int(now.Sub(state.LastOutputAt).Seconds())
			}
			state.Signals.LastUserChars = len(evt.Text)
			state.Turns = append(state.Turns, model.Turn{
				Role: "user",
				Text: evt.Text,
				TS:   now,
			})
		}
	}

	return state
}
