package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"bubble-talk/server/internal/actor"
	"bubble-talk/server/internal/director"
	"bubble-talk/server/internal/gateway"
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
	store          session.Store
	timeline       timeline.Store
	directorEngine *director.DirectorEngine
	actorEngine    *actor.ActorEngine
	now            func() time.Time
	logger         *log.Logger
}

// New 创建Orchestrator（兼容旧版本API）
func New(store session.Store, timeline timeline.Store, now func() time.Time) *Orchestrator {
	if now == nil {
		now = time.Now
	}

	// 使用默认配置创建Director和Actor
	directorEngine := director.NewDirectorEngine(nil, nil)
	actorEngine, err := actor.NewActorEngine("configs/prompts")
	if err != nil {
		log.Printf("Warning: failed to create actor engine: %v, using nil", err)
	}

	return &Orchestrator{
		store:          store,
		timeline:       timeline,
		directorEngine: directorEngine,
		actorEngine:    actorEngine,
		now:            now,
		logger:         log.Default(),
	}
}

// NewWithEngines 创建Orchestrator并指定Director和Actor引擎
func NewWithEngines(
	store session.Store,
	timeline timeline.Store,
	directorEngine *director.DirectorEngine,
	actorEngine *actor.ActorEngine,
	logger *log.Logger,
) *Orchestrator {
	if logger == nil {
		logger = log.Default()
	}

	return &Orchestrator{
		store:          store,
		timeline:       timeline,
		directorEngine: directorEngine,
		actorEngine:    actorEngine,
		now:            time.Now,
		logger:         logger,
	}
}

// GetInitialInstructions 生成会话初始的 System Instructions。
func (o *Orchestrator) GetInitialInstructions(_ context.Context, state *model.SessionState) (string, error) {
	// 如果actorEngine未初始化，返回简单的默认指令
	if o.actorEngine == nil {
		return "你是 BubbleTalk 的语音教学助手。默认用中文、口语化、短句输出。", nil
	}

	// 创建一个初始的DirectorPlan
	plan := o.directorEngine.Decide(state, "")

	// 通过Actor Engine构建Prompt
	req := actor.ActorRequest{
		SessionID:     state.SessionID,
		TurnID:        "initial",
		Plan:          plan,
		EntryID:       state.EntryID,
		Domain:        state.Domain,
		MainObjective: state.MainObjective,
		ConceptName:   state.MainObjective,
		LastUserText:  "",
		Metaphor:      "",
	}

	prompt, err := o.actorEngine.BuildPrompt(req)
	if err != nil {
		o.logger.Printf("Failed to build initial prompt: %v", err)
		// 使用兜底Prompt
		prompt = o.actorEngine.BuildFallbackPrompt(req)
	}

	return prompt.Instructions, nil
}

// HandleUserUtterance 处理用户语音转写输入
func (o *Orchestrator) HandleUserUtterance(ctx context.Context, sessionID string, text string, gw *gateway.Gateway) error {
	o.logger.Printf("[Orchestrator] handling user utterance for session %s: %s", sessionID, text)

	// 1. 获取当前会话状态
	state, err := o.store.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	// 2. 记录用户输入到Timeline
	event := &model.Event{
		EventID:   fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID: sessionID,
		Type:      "user_utterance",
		Text:      text,
		ClientTS:  o.now(),
		ServerTS:  o.now(),
	}
	if _, err := o.timeline.Append(ctx, sessionID, event); err != nil {
		o.logger.Printf("Failed to append timeline event: %v", err)
	}

	// 3. 调用Director生成计划
	plan := o.directorEngine.Decide(state, text)

	o.logger.Printf("[Orchestrator] Director plan: role=%s beat=%s action=%s",
		plan.NextRole, plan.NextBeat, plan.OutputAction)

	// 4. 记录Director计划到Timeline
	planEvent := &model.Event{
		EventID:      fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID:    sessionID,
		Type:         "director_plan",
		ServerTS:     o.now(),
		DirectorPlan: &plan,
	}
	if _, err := o.timeline.Append(ctx, sessionID, planEvent); err != nil {
		o.logger.Printf("Failed to append plan event: %v", err)
	}

	// 5. 调用Actor生成Prompt
	req := actor.ActorRequest{
		SessionID:     sessionID,
		TurnID:        event.EventID,
		Plan:          plan,
		EntryID:       state.EntryID,
		Domain:        state.Domain,
		MainObjective: state.MainObjective,
		ConceptName:   state.MainObjective,
		LastUserText:  text,
		Metaphor:      "", // TODO: 从ConceptPack获取
	}

	prompt, err := o.actorEngine.BuildPrompt(req)
	if err != nil {
		o.logger.Printf("Failed to build prompt: %v", err)
		prompt = o.actorEngine.BuildFallbackPrompt(req)
	}

	// 校验Prompt
	if err := o.actorEngine.Validate(prompt); err != nil {
		o.logger.Printf("Prompt validation failed: %v, using fallback", err)
		prompt = o.actorEngine.BuildFallbackPrompt(req)
	}

	o.logger.Printf("[Orchestrator] Actor prompt generated, length=%d", len(prompt.Instructions))

	// 6. 通过Gateway发送指令到Realtime
	if gw != nil {
		if err := gw.SendInstructions(ctx, prompt.Instructions, prompt.DebugInfo); err != nil {
			return fmt.Errorf("send instructions: %w", err)
		}
		o.logger.Printf("[Orchestrator] Instructions sent to Realtime successfully")
	}

	// 7. 更新会话状态
	state.LastUserUtterance = text
	state.OutputClockSec += int(time.Since(state.UpdatedAt).Seconds())
	state.UpdatedAt = o.now()

	if err := o.store.Save(ctx, state); err != nil {
		o.logger.Printf("Failed to update session: %v", err)
	}

	return nil
}

// HandleQuizAnswer 处理答题事件
func (o *Orchestrator) HandleQuizAnswer(ctx context.Context, sessionID string, questionID string, answer string) error {
	o.logger.Printf("[Orchestrator] quiz answer: session=%s question=%s answer=%s",
		sessionID, questionID, answer)

	// 记录到Timeline
	event := &model.Event{
		EventID:    fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID:  sessionID,
		Type:       "quiz_answer",
		QuestionID: questionID,
		Answer:     answer,
		ServerTS:   o.now(),
	}

	if _, err := o.timeline.Append(ctx, sessionID, event); err != nil {
		o.logger.Printf("Failed to append quiz answer: %v", err)
	}

	// TODO: 调用Assessment Engine评估答案
	// TODO: 更新Learning Model

	return nil
}

// HandleBargeIn 处理插话中断事件
func (o *Orchestrator) HandleBargeIn(ctx context.Context, sessionID string) error {
	o.logger.Printf("[Orchestrator] barge-in detected for session %s", sessionID)

	// 记录到Timeline
	event := &model.Event{
		EventID:   fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID: sessionID,
		Type:      "barge_in",
		ServerTS:  o.now(),
	}

	if _, err := o.timeline.Append(ctx, sessionID, event); err != nil {
		o.logger.Printf("Failed to append barge-in event: %v", err)
	}

	// TODO: 更新会话状态（记录中断次数，调整紧张度）

	return nil
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
