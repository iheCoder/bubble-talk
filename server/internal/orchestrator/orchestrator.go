package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"bubble-talk/server/internal/actor"
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/director"
	"bubble-talk/server/internal/gateway"
	"bubble-talk/server/internal/llm"
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

	// 使用默认配置创建Director和Actor（不启用LLM）
	cfg := &config.Config{
		Director: config.DirectorConfig{
			EnableLLM:              false,
			DefaultTalkBurstLimit:  20,
			HighLoadTalkBurstLimit: 15,
			OutputClockThreshold:   90,
		},
	}

	directorEngine := director.NewDirectorEngine(cfg, nil)
	actorEngine, err := actor.NewActorEngine("server/configs/prompts")
	if err != nil {
		log.Printf("❌ Warning: failed to create actor engine: %v, using nil", err)
		log.Printf("💡 Hint: Make sure to run from project root directory")
	} else {
		log.Printf("✅ ActorEngine initialized successfully")
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

// NewWithConfig 创建Orchestrator并使用完整配置（支持LLM）
func NewWithConfig(
	store session.Store,
	timeline timeline.Store,
	cfg *config.Config,
	now func() time.Time,
) (*Orchestrator, error) {
	if now == nil {
		now = time.Now
	}

	// 创建LLM客户端（如果启用）
	var llmClient llm.Client
	var err error
	if cfg.Director.EnableLLM {
		llmClient, err = llm.NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM client: %w", err)
		}
		log.Printf("✅ LLM client initialized (provider: %s)", cfg.LLM.Provider)
	}

	// 创建Director和Actor引擎
	directorEngine := director.NewDirectorEngine(cfg, llmClient)
	actorEngine, err := actor.NewActorEngine(cfg.Paths.Prompts)
	if err != nil {
		return nil, fmt.Errorf("failed to create actor engine: %w", err)
	}

	return &Orchestrator{
		store:          store,
		timeline:       timeline,
		directorEngine: directorEngine,
		actorEngine:    actorEngine,
		now:            now,
		logger:         log.Default(),
	}, nil
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
func (o *Orchestrator) HandleUserUtterance(ctx context.Context, sessionID string, text string, gw interface{}) error {
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

	// 2.1 关键：ASR 直通路径此前只写 Timeline，不归约 SessionState，
	// 会导致 Turns 不增长，从而导演的“轮流选角色”永远停在第一个角色（通常是 host）。
	Reduce(state, *event, o.now())
	state.LastUserUtterance = text

	// 3. 调用Director生成计划
	plan := o.directorEngine.Decide(state, text)

	o.logger.Printf("[Orchestrator] Director plan: role=%s", plan.NextRole)

	// 4. 记录Director计划到Timeline
	if err := o.appendDirectorPlan(ctx, sessionID, plan); err != nil {
		o.logger.Printf("Failed to append plan event: %v", err)
	}

	// 5. 调用Actor生成Prompt
	prompt := o.buildActorPrompt(state, plan, event.EventID, text)

	o.logger.Printf("[Orchestrator] Actor prompt generated, length=%d", len(prompt.Instructions))

	// 6. 通过Gateway发送指令到Realtime
	// 关键：在 metadata 中传递 role，MultiVoiceGateway 需要这个字段
	if gw != nil {
		metadata := map[string]interface{}{
			"role": plan.NextRole, // 关键！指定哪个角色说话
		}

		// 类型断言，支持两种 Gateway
		if mvg, ok := gw.(*gateway.MultiVoiceGateway); ok {
			if err := mvg.SendInstructions(ctx, prompt.Instructions, metadata); err != nil {
				return fmt.Errorf("send instructions to MultiVoiceGateway: %w", err)
			}
		} else if g, ok := gw.(*gateway.Gateway); ok {
			if err := g.SendInstructions(ctx, prompt.Instructions, metadata); err != nil {
				return fmt.Errorf("send instructions to Gateway: %w", err)
			}
		} else {
			o.logger.Printf("[Orchestrator] ⚠️  Unknown gateway type, skipping SendInstructions")
		}

		o.logger.Printf("[Orchestrator] Instructions sent to Realtime successfully")
	}

	// 7. 更新会话状态
	state.LastUserUtterance = text
	state.UpdatedAt = o.now()

	if err := o.store.Save(ctx, state); err != nil {
		o.logger.Printf("Failed to update session: %v", err)
	}

	return nil
}

// HandleAssistantText 处理一次助手输出完成后的文本（用于 Timeline/SessionState 归约）。
//
// 契约：
// - 只做事实记录，不触发 Director/Actor（避免重复驱动输出）
// - 让 Director 能基于 assistantTurns 做角色轮转
func (o *Orchestrator) HandleAssistantText(ctx context.Context, sessionID string, text string, fromRole string) error {
	if text == "" {
		return nil
	}

	state, err := o.store.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	event := &model.Event{
		EventID:   fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID: sessionID,
		Type:      "assistant_text",
		Text:      text,
		ServerTS:  o.now(),
	}
	if _, err := o.timeline.Append(ctx, sessionID, event); err != nil {
		return fmt.Errorf("append timeline event: %w", err)
	}

	Reduce(state, *event, o.now())
	state.UpdatedAt = o.now()

	// 预留：未来可将 fromRole 写入更结构化的字段，便于审计/回放。
	_ = fromRole

	if err := o.store.Save(ctx, state); err != nil {
		return fmt.Errorf("save session: %w", err)
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

// HandleWorldEntered 处理进入 World 的事件，导演主动开场。
func (o *Orchestrator) HandleWorldEntered(ctx context.Context, sessionID string, gw interface{}) error {
	o.logger.Printf("[Orchestrator] world entered: session=%s", sessionID)

	state, err := o.store.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	eventID := fmt.Sprintf("evt_%d", o.now().UnixNano())
	event := &model.Event{
		EventID:   eventID,
		SessionID: sessionID,
		Type:      "world_entered",
		ClientTS:  o.now(),
		ServerTS:  o.now(),
	}
	if _, err := o.timeline.Append(ctx, sessionID, event); err != nil {
		o.logger.Printf("Failed to append world_entered event: %v", err)
	}

	plan := o.directorEngine.Decide(state, "")
	if err := o.appendDirectorPlan(ctx, sessionID, plan); err != nil {
		o.logger.Printf("Failed to append plan event: %v", err)
	}

	prompt := o.buildActorPrompt(state, plan, eventID, "")

	// 通过 Gateway 发送指令
	if gw != nil {
		metadata := map[string]interface{}{
			"role": plan.NextRole, // 关键！指定哪个角色说话
		}

		// 类型断言，支持两种 Gateway
		if mvg, ok := gw.(*gateway.MultiVoiceGateway); ok {
			if err := mvg.SendInstructions(ctx, prompt.Instructions, metadata); err != nil {
				return fmt.Errorf("send instructions to MultiVoiceGateway: %w", err)
			}
		} else if g, ok := gw.(*gateway.Gateway); ok {
			if err := g.SendInstructions(ctx, prompt.Instructions, metadata); err != nil {
				return fmt.Errorf("send instructions to Gateway: %w", err)
			}
		} else {
			o.logger.Printf("[Orchestrator] ⚠️  Unknown gateway type, skipping SendInstructions")
		}

		o.logger.Printf("[Orchestrator] Opening instructions sent successfully")
	}

	state.UpdatedAt = o.now()
	if err := o.store.Save(ctx, state); err != nil {
		o.logger.Printf("Failed to update session: %v", err)
	}

	return nil
}

func (o *Orchestrator) appendDirectorPlan(ctx context.Context, sessionID string, plan model.DirectorPlan) error {
	planEvent := &model.Event{
		EventID:      fmt.Sprintf("evt_%d", o.now().UnixNano()),
		SessionID:    sessionID,
		Type:         "director_plan",
		ServerTS:     o.now(),
		DirectorPlan: &plan,
	}
	_, err := o.timeline.Append(ctx, sessionID, planEvent)
	return err
}

func (o *Orchestrator) buildActorPrompt(
	state *model.SessionState,
	plan model.DirectorPlan,
	turnID string,
	lastUserText string,
) actor.ActorPrompt {
	req := actor.ActorRequest{
		SessionID:     state.SessionID,
		TurnID:        turnID,
		Plan:          plan,
		EntryID:       state.EntryID,
		Domain:        state.Domain,
		MainObjective: state.MainObjective,
		ConceptName:   state.MainObjective,
		LastUserText:  lastUserText,
		Metaphor:      "", // TODO: 从ConceptPack获取
	}

	prompt, err := o.actorEngine.BuildPrompt(req)
	if err != nil {
		o.logger.Printf("Failed to build prompt: %v", err)
		prompt = o.actorEngine.BuildFallbackPrompt(req)
	}
	if err := o.actorEngine.Validate(prompt); err != nil {
		o.logger.Printf("Prompt validation failed: %v, using fallback", err)
		prompt = o.actorEngine.BuildFallbackPrompt(req)
	}
	return prompt
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
	// TODO 后续接入 ActorEngine 时，这里应返回 ActorReply，而不是简单的 Assistant 文本。
	plan := model.DirectorPlan{
		NextRole:    "Coach",
		Instruction: "User Mind State: Partial\nNext Beat: Check\nOutput Action: Recap\n",
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
