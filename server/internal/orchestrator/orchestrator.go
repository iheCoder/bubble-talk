package orchestrator

import (
	"context"
	"time"

	"bubble-talk/server/internal/model"
	"bubble-talk/server/internal/session"
	"bubble-talk/server/internal/timeline"
)

// Orchestrator 负责处理会话事件的编排逻辑。
//
// 职责与契约：
// - append-first：任何输入先写 Timeline，再做 reduce，保证可回放与幂等。
// - 决策集中：Director/Actor/Assessment 的裁决都应在此触发，避免分散到网关/前端。
// - 输出可审计：助手输出与计划要写回 Timeline，以便验收/复盘。
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

// GetInitialInstructions 生成会话初始的 System Instructions。
// 这是 ActorEngine 的一部分职责，用于初始化 GPT Realtime 的人设和目标。
func (o *Orchestrator) GetInitialInstructions(state *model.SessionState) string {
	// TODO: 这里应该调用 ActorEngine 的 PromptBuilder
	// 目前先硬编码，但结构上已经解耦
	return "你是 BubbleTalk 的语音教学助手。默认用中文、口语化、短句输出。" +
		"本次泡泡主题：" + state.EntryID + "。当前主目标：" + state.MainObjective + "。" +
		"对话规则：每 90 秒必须让用户完成一次输出动作（复述/选择/举例/迁移）。" +
		"如果用户说“我懂了/结束”，必须立刻给出迁移检验（Exit Ticket）。"
}

// OnEvent 处理来自用户或系统的事件，更新会话状态并生成响应。
//
// 副作用说明：
// - 追加事实事件到 Timeline（append-first）。
// - 归约并更新 Session 快照（便于后续增量处理）。
// - 写入 director_plan 与 assistant_text，作为可审计的输出事实。
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

	// 第一阶段：DirectorPlan 与 Actor 输出先用 stub，确保编排流水线可验收。
	// 后续接入 ActorEngine 时，这里应返回 ActorReply，而不是简单的 Assistant 文本。
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

	// 临时台词：用于验证“事件流 + 语音播报”闭环。
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
