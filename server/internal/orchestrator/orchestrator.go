package orchestrator

import (
	"context"
	"time"

	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"
)

// Orchestrator 负责处理会话事件的编排逻辑。
// 职责：执行 append-first 流水线，保证事件可回放、状态可重建。
type Orchestrator struct {
	store    session.Store
	timeline timeline.Store
	now      func() time.Time
}

func New(store session.Store, timeline timeline.Store, now func() time.Time) *Orchestrator {
	if now == nil {
		now = time.Now
	}
	return &Orchestrator{
		store:    store,
		timeline: timeline,
		now:      now,
	}
}

// OnEvent 处理来自用户或系统的事件，更新会话状态并生成响应。
// 副作用：写入 Timeline 与 Session 快照，确保“说过的话都有迹可循”。
func (o *Orchestrator) OnEvent(ctx context.Context, sessionID string, evt model.Event) (*model.EventResponse, error) {
	state, err := o.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	now := o.now()
	normalized := normalizeEvent(sessionID, evt, now)
	// append-first：先写事实，再归约快照，避免“说了但没记”。
	seq, err := o.timeline.Append(ctx, sessionID, &normalized)
	if err != nil {
		return nil, err
	}
	normalized.Seq = seq

	Reduce(state, normalized, now)
	if err := o.store.Save(ctx, state); err != nil {
		return nil, err
	}

	// 第一阶段：导演计划与演员输出先用 stub，确保编排流水线可验收。
	plan := model.DirectorPlan{
		UserMindState:     []string{"Partial"},
		Intent:            "Clarify",
		NextBeat:          "Check",
		NextRole:          "Coach",
		OutputAction:      "Recap",
		TalkBurstLimitSec: 20,
		TensionGoal:       "keep",
		LoadGoal:          "keep",
		StackAction:       "keep",
		Notes:             "stub plan for stage-1",
	}

	planEvent := model.Event{
		Type:         "director_plan",
		DirectorPlan: &plan,
		ServerTS:     now,
	}
	if _, err := o.timeline.Append(ctx, sessionID, &planEvent); err != nil {
		return nil, err
	}

	assistantText := "收到。先用一句话复述你的理解，我们再往下走。"
	assistantEvent := model.Event{
		Type:     "assistant_text",
		Text:     assistantText,
		ServerTS: now,
	}
	if _, err := o.timeline.Append(ctx, sessionID, &assistantEvent); err != nil {
		return nil, err
	}

	Reduce(state, assistantEvent, now)
	if err := o.store.Save(ctx, state); err != nil {
		return nil, err
	}

	resp := model.EventResponse{
		Assistant: model.AssistantMessage{
			Text: assistantText,
			NeedUserAction: &model.UserAction{
				Type:   "recap",
				Prompt: "用一句话复述，必须包含因为…所以…",
			},
			Quiz: nil,
		},
		Debug: &model.DebugPayload{DirectorPlan: plan},
	}

	return &resp, nil
}

func normalizeEvent(sessionID string, evt model.Event, now time.Time) model.Event {
	// 兼容性：旧客户端可能不传 type/client_ts，补齐默认值。
	if evt.Type == "" {
		evt.Type = "user_message"
	}
	if evt.ClientTS.IsZero() {
		evt.ClientTS = now
	}
	evt.ServerTS = now
	evt.SessionID = sessionID
	return evt
}
