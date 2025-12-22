package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/llm"
	"bubble-talk/server/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// DirectorEngine 导演引擎
// 负责选择下一个 Beat（拍点）和 Role（角色）
// 支持规则引擎（rule-based）和 LLM 驱动两种模式
type DirectorEngine struct {
	config         *config.DirectorConfig
	llmClient      llm.Client
	beatLibrary    map[string]*BeatCard
	availableRoles []string
	availableBeats []string
}

// BeatCard 拍点指令卡
type BeatCard struct {
	BeatID             string   `json:"beat_id"`
	Goal               string   `json:"goal"`
	UserMustDoType     string   `json:"user_must_do_type"`
	TalkBurstLimitHint int      `json:"talk_burst_limit_hint"`
	ExitCondition      string   `json:"exit_condition"`
	NextSuggest        []string `json:"next_suggest"`
}

// decisionPlan 是导演内部使用的完整决策载体。
// 对外只暴露精简的 DirectorPlan（角色 + 指令 + Debug）。
type decisionPlan struct {
	FlowMode          string
	UserMindState     []string
	Intent            string
	NextBeat          string
	NextRole          string
	OutputAction      string
	ContentDirection  string
	TalkBurstLimitSec int
	TensionGoal       string
	LoadGoal          string
	Notes             string
	Debug             *model.DirectorDebug
}

// NewDirectorEngine 创建导演引擎
func NewDirectorEngine(cfg *config.Config, llmClient llm.Client) *DirectorEngine {
	roles := cfg.Director.AvailableRoles
	if len(roles) == 0 {
		roles = []string{"host", "economist", "skeptic"}
	}

	beats := cfg.Director.AvailableBeats
	if len(beats) == 0 {
		beats = []string{
			"reveal", "check", "deepen", "twist", "continue",
			"lens_shift", "feynman", "montage", "minigame", "exit_ticket",
		}
	}

	return &DirectorEngine{
		config:         &cfg.Director,
		llmClient:      llmClient,
		beatLibrary:    initBeatLibrary(),
		availableRoles: roles,
		availableBeats: beats,
	}
}

// Decide 生成导演计划
// 这是导演引擎的核心方法，负责决定下一个拍点和角色
func (d *DirectorEngine) Decide(state *model.SessionState, userInput string) model.DirectorPlan {
	ctx := context.Background()

	var decision decisionPlan

	// 如果启用 LLM，让 LLM 完全负责推断（包括 FlowMode）
	if d.config.EnableLLM && d.llmClient != nil {
		llmDecision, err := d.decideLLM(ctx, state, userInput)
		if err != nil {
			log.Printf("⚠️ LLM decision failed, falling back to rules: %v", err)
			// 降级到规则引擎
			flowMode := d.inferFlowMode(state, userInput)
			userMindState := d.inferUserMindState(state, userInput)
			beatCandidates := d.generateBeatCandidates(state, flowMode, userMindState)
			decision = d.decideWithRules(state, flowMode, userMindState, beatCandidates)
		} else {
			decision = llmDecision
		}
	} else {
		// 使用规则引擎：先推断 FlowMode 和 UserMindState，再生成候选
		flowMode := d.inferFlowMode(state, userInput)
		userMindState := d.inferUserMindState(state, userInput)
		beatCandidates := d.generateBeatCandidates(state, flowMode, userMindState)
		decision = d.decideWithRules(state, flowMode, userMindState, beatCandidates)
	}

	// Layer A: 应用硬约束（最后验证）
	decision = d.applyGuardrails(decision, state)

	return model.DirectorPlan{
		NextRole:    decision.NextRole,
		Instruction: d.buildInstruction(state, userInput, decision),
		Debug:       decision.Debug,
	}
}

// buildInstruction 将内部决策渲染为可执行的导演指令文本。
// TODO 是不是考虑更加完善的提示词，以方便actor更好地执行呢？比如TensionGoal说提升和降低是不是对于角色过于抽象呢？
func (d *DirectorEngine) buildInstruction(
	state *model.SessionState,
	userInput string,
	decision decisionPlan,
) string {
	_ = state
	_ = userInput

	// Director → Actor 的指令保持极短，避免“写成状态面板”导致执行发散：
	// State / Beat / Do / Direction
	//
	// 说明：
	// - 主线目标与用户输入属于 [Context]，这里不重复。
	// - 不输出 FlowMode；UserMindState 已覆盖“顺流/救场”的语义。

	stateLine := formatMindState(decision.UserMindState)
	beatLine := decision.NextBeat
	interactiveMode := directiveForBeat(decision.NextBeat)
	directionLine := decision.ContentDirection
	if strings.TrimSpace(directionLine) == "" {
		directionLine = defaultContentDirection(decision.NextBeat, decision.UserMindState)
	}

	return fmt.Sprintf(
		"State: %s\nBeat: %s\ninteractive Mode: %s\nDirection: %s\n",
		stateLine,
		beatLine,
		interactiveMode,
		directionLine,
	)
}

func formatMindState(states []string) string {
	if len(states) == 0 {
		return "Engaged"
	}

	// 默认 Engaged 是“兜底标签”；如果同时出现更具体的状态，则去掉 Engaged。
	if len(states) > 1 {
		filtered := make([]string, 0, len(states))
		for _, s := range states {
			if s != "Engaged" {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) > 0 {
			states = filtered
		}
	}
	return strings.Join(states, ", ")
}

func directiveForBeat(beat string) (doLine string) {
	switch beat {
	case "continue":
		return "顺着用户的表达推进一步：用口语化短句解释，讲一小步就停，留出用户回应空间。"
	case "check":
		return "用一个简短问题检验理解，逼出用户输出；问完停下来等用户回答。"
	case "reveal":
		return "用一个生活化比喻解释核心概念：避免术语，控制在 1–2 句；然后用一句短问句确认是否听懂。"
	case "deepen":
		return "追问机制/因果链：让用户把关键连接说出来；只追一条链，不要铺开。"
	case "twist":
		return "用一个反例/边界案例挑战用户假设：制造“看起来矛盾”的瞬间，然后让用户解释为什么成立/不成立。"
	case "lens_shift":
		return "换一个视角重述并对比两种解释，澄清边界；用对比让用户自己选更贴近的一种。"
	case "feynman":
		return "让用户用“教小白”的方式复述；你只负责听漏洞并轻轻补齐，不要替用户讲完。"
	case "montage":
		return "给 2–3 个快速场景，让用户找共同模式，促进迁移；每个场景一句话就够。"
	case "minigame":
		return "做一个小互动练习，降低负荷，把节奏拉回对话；规则一句话说清，立刻让用户动手。"
	case "exit_ticket":
		return "给一道迁移题，检验能否应用到新情境，要求具体回答；问完停下来等用户回答。"
	default:
		return ""
	}
}

func defaultContentDirection(beat string, states []string) string {
	// 规则/降级路径下的“内容方向”只给通用写作约束，不注入具体题材，避免硬编码。
	// LLM 模式下由模型结合上下文与用户画像自行产出。
	switch beat {
	case "twist":
		return "选一个与用户最近一句相关的反例/边界案例（贴近用户背景与语境），让结论在该案例下显得不成立，从而逼出澄清。"
	case "reveal":
		return "选一个用户熟悉的日常场景做比喻（贴近用户语境），把抽象概念映射到具体选择/代价/收益。"
	case "check":
		return "围绕本轮核心点提一个最小问题（只有一个关键变量），问题要能让用户明确输出。"
	case "continue":
		if containsState(states, "Engaged") {
			return "顺着用户兴趣点自由推进一小步：用一个贴近语境的例子承载概念，不要突然换题材。"
		}
		return "沿着最近上下文推进一小步，用贴近语境的例子承载概念。"
	default:
		return "优先沿用最近上下文与用户熟悉的素材承载本轮策略，尽量具体、可代入。"
	}
}

// inferFlowMode 推断流动模式（FLOW 或 RESCUE）
func (d *DirectorEngine) inferFlowMode(state *model.SessionState, userInput string) string {
	// RESCUE 信号：
	// 1. 有误解标签
	// 2. 掌握度低
	// 3. 认知负荷或张力过高
	// 4. 用户输入显示困惑

	if len(state.MisconceptionTags) > 0 {
		return "RESCUE"
	}

	if state.MasteryEstimate < 0.4 {
		return "RESCUE"
	}

	if state.CognitiveLoad > 7 || state.TensionLevel > 7 {
		return "RESCUE"
	}

	// 检查用户输入是否显示困惑
	confusionKeywords := []string{"不懂", "不明白", "什么意思", "confused", "don't understand"}
	lowerInput := strings.ToLower(userInput)
	for _, keyword := range confusionKeywords {
		if strings.Contains(lowerInput, keyword) {
			return "RESCUE"
		}
	}

	// 默认为 FLOW（顺流）
	return "FLOW"
}

// inferUserMindState 推断用户心理状态
// 根据文档 4.3，支持 8 种状态：Fog, Illusion, Partial, Aha, Verify, Expand, Fatigue, Flow
func (d *DirectorEngine) inferUserMindState(state *model.SessionState, userInput string) []string {
	states := make([]string, 0)

	// Fatigue（疲惫）：输出变短，响应延迟长
	if state.Signals.LastUserChars < 10 && state.Signals.LastUserLatencyMS > 5000 {
		states = append(states, "Fatigue")
		return states // Fatigue 优先级最高
	}

	// Fog（迷雾）：有误解标签 + 认知负荷高
	if len(state.MisconceptionTags) > 0 && state.CognitiveLoad > 6 {
		states = append(states, "Fog")
	}

	// Illusion（错觉）：掌握度低但张力低（假装懂了）
	if state.MasteryEstimate < 0.4 && state.TensionLevel < 4 {
		states = append(states, "Illusion")
	}

	// Partial（半懂）：掌握度中等
	if state.MasteryEstimate >= 0.4 && state.MasteryEstimate < 0.7 {
		states = append(states, "Partial")
	}

	// Aha（顿悟）：掌握度突然提升（检测需要历史数据，简化处理）
	if state.MasteryEstimate >= 0.7 && len(state.MisconceptionTags) == 0 {
		states = append(states, "Aha")
	}

	// Verify（求证）：用户主动提问（检查问号）
	if strings.Contains(userInput, "?") || strings.Contains(userInput, "？") {
		states = append(states, "Verify")
	}

	// Expand（外扩）：用户提到案例、例子
	expandKeywords := []string{"例如", "比如", "举例", "案例", "example", "for instance"}
	lowerInput := strings.ToLower(userInput)
	for _, keyword := range expandKeywords {
		if strings.Contains(lowerInput, keyword) {
			states = append(states, "Expand")
			break
		}
	}

	// 默认为 Engaged（参与中）
	if len(states) == 0 {
		states = append(states, "Engaged")
	}

	return states
}

// generateBeatCandidates 生成候选拍点
// 根据硬约束和用户状态缩小搜索空间
func (d *DirectorEngine) generateBeatCandidates(state *model.SessionState, flowMode string, userMindState []string) []string {
	candidates := make([]string, 0)

	// 硬约束 1：output_clock >= 90 秒，强制输出型 Beat
	if state.OutputClockSec >= d.config.OutputClockThreshold {
		return []string{"check", "feynman", "exit_ticket"}
	}

	// 硬约束 2：疲惫状态，降低负荷
	if containsState(userMindState, "Fatigue") {
		return []string{"minigame", "exit_ticket"}
	}

	// 根据 FlowMode 和 UserMindState 生成候选
	if flowMode == "FLOW" {
		// 顺流模式：优先 continue, deepen, check（小步推进）
		candidates = append(candidates, "continue", "deepen", "check")
	} else {
		// 救场模式：根据具体状态选择
		if containsState(userMindState, "Fog") {
			candidates = append(candidates, "reveal", "lens_shift")
		}
		if containsState(userMindState, "Illusion") {
			candidates = append(candidates, "twist", "check")
		}
		if containsState(userMindState, "Partial") {
			candidates = append(candidates, "lens_shift", "deepen")
		}
		if containsState(userMindState, "Aha") {
			candidates = append(candidates, "feynman", "check")
		}
		if containsState(userMindState, "Verify") {
			candidates = append(candidates, "deepen", "check")
		}
		if containsState(userMindState, "Expand") {
			candidates = append(candidates, "montage", "deepen")
		}
	}

	// 确保候选集不为空
	if len(candidates) == 0 {
		candidates = []string{"continue", "check"}
	}

	return dedup(candidates)
}

// decideLLM 使用 LLM 进行决策
// LLM 模式下，让 LLM 完全负责推断 FlowMode、UserMindState 和选择 Beat/Role
func (d *DirectorEngine) decideLLM(
	ctx context.Context,
	state *model.SessionState,
	userInput string,
) (decisionPlan, error) {
	// 构建提示词
	systemPrompt := d.buildSystemPrompt()
	userPrompt := d.buildUserPromptForLLM(state, userInput)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// 定义 JSON Schema（完全符合 OpenAI Structured Outputs 严格要求）
	// 注意：OpenAI Strict Mode 要求 required 包含所有 properties 中的字段
	// 所以我们将可选字段（user_must_do, debug）从 properties 中移出
	schema := &llm.JSONSchema{
		Name: "director_plan",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				// 必填字段
				"flow_mode": map[string]any{
					"type":        "string",
					"enum":        []string{"FLOW", "RESCUE"},
					"description": "流动模式：FLOW(顺流) 或 RESCUE(救场)",
				},
				"user_mind_state": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "用户心理状态列表，如Partial,Verify,Fog等",
				},
				"intent": map[string]any{
					"type":        "string",
					"description": "对话意图，如clarify,deepen等",
				},
				"next_beat": map[string]any{
					"type":        "string",
					"description": "下一个拍点，必须在可用拍点列表中",
				},
				"next_role": map[string]any{
					"type":        "string",
					"description": "下一个角色，必须在可用角色列表中",
				},
				"output_action": map[string]any{
					"type":        "string",
					"description": "输出动作，如ask_simple_question等",
				},
				"content_direction": map[string]any{
					"type":        "string",
					"description": "内容方向参考：基于对话主题、对话上下文、以及对用户的了解描述下角色本轮要说的大概内容方向。",
				},
				"talk_burst_limit_sec": map[string]any{
					"type":        "integer",
					"description": "说话时长限制，单位秒",
				},
				"tension_goal": map[string]any{
					"type":        "string",
					"enum":        []string{"increase", "maintain", "decrease"},
					"description": "张力目标",
				},
				"load_goal": map[string]any{
					"type":        "string",
					"enum":        []string{"increase", "maintain", "decrease"},
					"description": "负荷目标",
				},
				"notes": map[string]any{
					"type":        "string",
					"description": "决策说明和推理过程",
				},
			},
			"required": []string{
				"flow_mode",
				"user_mind_state",
				"intent",
				"next_beat",
				"next_role",
				"output_action",
				"content_direction",
				"talk_burst_limit_sec",
				"tension_goal",
				"load_goal",
				"notes",
			},
			"additionalProperties": false,
		},
		Strict: true,
	}

	// 调用 LLM
	response, err := d.llmClient.Complete(ctx, messages, schema)
	if err != nil {
		return decisionPlan{}, fmt.Errorf("LLM complete: %w", err)
	}

	// 解析响应
	var planData struct {
		FlowMode          string   `json:"flow_mode"`
		UserMindState     []string `json:"user_mind_state"`
		Intent            string   `json:"intent"`
		NextBeat          string   `json:"next_beat"`
		NextRole          string   `json:"next_role"`
		OutputAction      string   `json:"output_action"`
		ContentDirection  string   `json:"content_direction"`
		TalkBurstLimitSec int      `json:"talk_burst_limit_sec"`
		TensionGoal       string   `json:"tension_goal"`
		LoadGoal          string   `json:"load_goal"`
		Notes             string   `json:"notes"`
	}

	if err := json.Unmarshal([]byte(response), &planData); err != nil {
		return decisionPlan{}, fmt.Errorf("unmarshal LLM response: %w", err)
	}

	// 构建内部决策
	plan := decisionPlan{
		FlowMode:          planData.FlowMode,
		UserMindState:     planData.UserMindState,
		Intent:            planData.Intent,
		NextBeat:          planData.NextBeat,
		NextRole:          planData.NextRole,
		OutputAction:      planData.OutputAction,
		ContentDirection:  planData.ContentDirection,
		TalkBurstLimitSec: planData.TalkBurstLimitSec,
		TensionGoal:       planData.TensionGoal,
		LoadGoal:          planData.LoadGoal,
		Notes:             planData.Notes,
	}

	return plan, nil
}

// decideWithRules 使用规则引擎进行决策
func (d *DirectorEngine) decideWithRules(
	state *model.SessionState,
	flowMode string,
	userMindState []string,
	beatCandidates []string,
) decisionPlan {
	// 从候选中选择第一个（简单策略）
	nextBeat := beatCandidates[0]

	// 选择角色（轮换）
	nextRole := d.selectNextRole(state)

	// 确定输出动作
	outputAction := d.determineOutputAction(nextBeat)

	return decisionPlan{
		FlowMode:          flowMode,
		UserMindState:     userMindState,
		Intent:            "clarify",
		NextBeat:          nextBeat,
		NextRole:          nextRole,
		OutputAction:      outputAction,
		ContentDirection:  defaultContentDirection(nextBeat, userMindState),
		TalkBurstLimitSec: d.determineTalkBurstLimit(state),
		TensionGoal:       d.determineTensionGoal(state),
		LoadGoal:          d.determineLoadGoal(state),
		Notes:             "规则引擎选择",
		Debug: &model.DirectorDebug{
			BeatCandidates:   beatCandidates,
			BeatChoiceReason: "规则：选择第一个候选拍点",
		},
	}
}

// applyGuardrails 应用硬约束
func (d *DirectorEngine) applyGuardrails(plan decisionPlan, state *model.SessionState) decisionPlan {
	// 验证 next_beat 在可用列表中
	if !contains(d.availableBeats, plan.NextBeat) {
		log.Printf("⚠️ Invalid beat '%s', falling back to 'check'", plan.NextBeat)
		plan.NextBeat = "check"
	}

	// 验证 next_role 在可用角色中
	availableRoles := state.AvailableRoles
	if len(availableRoles) == 0 {
		availableRoles = d.availableRoles
	}
	if !contains(availableRoles, plan.NextRole) {
		log.Printf("⚠️ Invalid role '%s', falling back to first available role", plan.NextRole)
		plan.NextRole = availableRoles[0]
	}

	return plan
}

// Helper methods

func (d *DirectorEngine) selectNextRole(state *model.SessionState) string {
	roles := state.AvailableRoles
	if len(roles) == 0 {
		roles = d.availableRoles
	}

	// 轮流选择角色
	assistantTurns := 0
	for _, turn := range state.Turns {
		if turn.Role == "assistant" {
			assistantTurns++
		}
	}

	roleIndex := assistantTurns % len(roles)
	return roles[roleIndex]
}

func (d *DirectorEngine) determineOutputAction(beat string) string {
	actionMap := map[string]string{
		"reveal":      "explain_with_metaphor",
		"check":       "ask_simple_question",
		"deepen":      "ask_elaboration",
		"twist":       "challenge_assumption",
		"continue":    "acknowledge_and_continue",
		"lens_shift":  "reframe_perspective",
		"feynman":     "ask_teach_back",
		"montage":     "show_multiple_examples",
		"minigame":    "engage_interactive",
		"exit_ticket": "assess_transfer",
	}
	if action, ok := actionMap[beat]; ok {
		return action
	}
	return "continue_dialogue"
}

func (d *DirectorEngine) determineTalkBurstLimit(state *model.SessionState) int {
	if state.CognitiveLoad > 7 || state.TensionLevel > 7 {
		return d.config.HighLoadTalkBurstLimit
	}
	return d.config.DefaultTalkBurstLimit
}

func (d *DirectorEngine) determineTensionGoal(state *model.SessionState) string {
	if state.TensionLevel < 4 {
		return "increase"
	} else if state.TensionLevel > 7 {
		return "decrease"
	}
	return "maintain"
}

func (d *DirectorEngine) determineLoadGoal(state *model.SessionState) string {
	if state.CognitiveLoad < 4 {
		return "increase"
	} else if state.CognitiveLoad > 7 {
		return "decrease"
	}
	return "maintain"
}

// buildSystemPrompt 构建系统提示词
func (d *DirectorEngine) buildSystemPrompt() string {
	return `你是一个专业的对话导演（Director），负责完全自主地决定下一个拍点（Beat）和角色（Role）。

你的职责：
1. **推断 FlowMode**：判断用户是顺流（FLOW）还是需要救场（RESCUE）
2. **推断 UserMindState**：识别用户的心理状态（可多选）
3. **选择最合适的拍点（Beat）**：基于状态和硬约束
4. **选择最合适的角色（Role）**：在泡泡固定角色集合中选择
5. **给出内容方向**：基于对话主题、对话上下文、以及对用户的了解描述下角色本轮要说的大概内容方向。

关键原则：
- 你有完全的推断自主权，不依赖预设的状态
- 必须尊重硬约束（如 output_clock ≥ 90 秒必须选输出型 beat）
- 选择必须在可用集合内（beats 和 roles）
- 保持结构化输出（严格 JSON）

输出格式要求：
- 严格按照 JSON Schema 返回
- 所有必填字段必须填写
- flow_mode 和 user_mind_state 是你自主推断的结果，不是输入

记住：你是导演，你决定一切。数据只是参考，最终决策权在你。`
}

// buildUserPrompt 构建用户提示词（规则引擎模式）
func (d *DirectorEngine) buildUserPrompt(
	state *model.SessionState,
	userInput string,
	flowMode string,
	userMindState []string,
	beatCandidates []string,
) string {
	// 构建拍点候选描述
	beatDescs := make([]string, 0, len(beatCandidates))
	for _, beatID := range beatCandidates {
		if card, ok := d.beatLibrary[beatID]; ok {
			beatDescs = append(beatDescs, fmt.Sprintf("- %s: %s (用户需: %s)", beatID, card.Goal, card.UserMustDoType))
		}
	}

	// 构建角色列表
	availableRoles := state.AvailableRoles
	if len(availableRoles) == 0 {
		availableRoles = d.availableRoles
	}

	return fmt.Sprintf(`## 当前状态面板

**Flow Mode**: %s
**User Mind State**: %s
**Mastery**: %.2f
**Misconceptions**: %v
**Output Clock**: %d 秒
**Tension**: %d
**Cognitive Load**: %d

**用户最新输入**: "%s"

**最近 2 轮对话**:
%s

## 候选拍点（你只能从中选择）

%s

## 可用角色

%s

## 任务

请选择最合适的 Beat 和 Role，并明确用户必须完成的输出动作。`,
		flowMode,
		strings.Join(userMindState, ", "),
		state.MasteryEstimate,
		state.MisconceptionTags,
		state.OutputClockSec,
		state.TensionLevel,
		state.CognitiveLoad,
		userInput,
		d.formatRecentTurns(state),
		strings.Join(beatDescs, "\n"),
		strings.Join(availableRoles, ", "),
	)
}

// buildUserPromptForLLM 构建用户提示词（LLM 模式 - 让 LLM 完全自主推断）
func (d *DirectorEngine) buildUserPromptForLLM(
	state *model.SessionState,
	userInput string,
) string {
	// 构建所有可用拍点的描述
	availableBeats := d.availableBeats
	beatDescs := make([]string, 0, len(availableBeats))
	for _, beatID := range availableBeats {
		if card, ok := d.beatLibrary[beatID]; ok {
			beatDescs = append(beatDescs, fmt.Sprintf("- **%s**: %s (用户需: %s, 时长: %ds)",
				beatID, card.Goal, card.UserMustDoType, card.TalkBurstLimitHint))
		}
	}

	// 构建角色列表
	availableRoles := state.AvailableRoles
	if len(availableRoles) == 0 {
		availableRoles = d.availableRoles
	}

	return fmt.Sprintf(`## 当前状态面板

**Mastery Estimate**: %.2f (0-1, 越高表示理解越好)
**Misconception Tags**: %v (用户的误解标签)
**Output Clock**: %d 秒 (已讲解时长，≥90 秒需强制用户输出)
**Tension Level**: %d (1-10, 用户紧张程度)
**Cognitive Load**: %d (1-10, 用户认知负荷)

**用户信号**:
- 最近输出长度: %d 字符
- 响应延迟: %d 毫秒

**用户最新输入**: "%s"

**最近对话历史**:
%s

---

## 可用拍点库

%s

## 可用角色

%s

---

## 你的任务

作为导演，你需要：

1. **推断 FlowMode**:
   - **FLOW**: 用户理解顺畅，无需救场（掌握度≥0.4，无误解，负荷不高）
   - **RESCUE**: 检测到问题，需要调整（有误解/掌握度低/负荷高/困惑）

2. **推断 UserMindState**（可多选）:
   - **Fog**: 迷雾（有误解 + 认知负荷高）
   - **Illusion**: 错觉（掌握度低但假装懂了，如"嗯/懂了"）
   - **Partial**: 半懂（掌握度 0.4-0.7）
   - **Aha**: 顿悟（掌握度≥0.7 且无误解）
   - **Verify**: 求证（用户主动提问，有"?"）
   - **Expand**: 外扩（提到例子、案例）
   - **Fatigue**: 疲惫（输出短<10字 且延迟长>5000ms）
   - **Engaged**: 参与（默认状态）

3. **选择合适的拍点（Beat）**:
   - 如果 Output Clock ≥ 90 秒，**必须**选择 check/feynman/exit_ticket
   - 如果 Fatigue，选择 minigame/exit_ticket
   - 如果 FLOW，优先 continue/deepen/check
   - 如果 RESCUE + Fog，选择 reveal/lens_shift
   - 如果 RESCUE + Illusion，选择 twist/check
   - 其他根据状态灵活选择

4. **选择合适的角色（Role）**:
   - 在 available_roles 中选择
   - 根据拍点和用户状态匹配

5. **生成内容方向（content_direction）**:
   - 基于对话主题、对话上下文描述下角色本轮要说的大概内容方向。”
   - 参考用户最新输入与最近对话上下文（不要过于空泛、不要复述目标）
   - 结合用户画像/领域偏好（如果已知），选择更容易代入的素材

5. **决定张力目标（tension_goal）**:
   - **increase**: 当前张力低于 4
   - **decrease**: 当前张力高于 7
   - **maintain**: 否则保持

6. **决定负荷目标（load_goal）**:
   - **increase**: 当前负荷低于 4
   - **decrease**: 当前负荷高于 7
   - **maintain**: 否则保持

请严格按照 JSON Schema 返回决策。`,
		state.MasteryEstimate,
		state.MisconceptionTags,
		state.OutputClockSec,
		state.TensionLevel,
		state.CognitiveLoad,
		state.Signals.LastUserChars,
		state.Signals.LastUserLatencyMS,
		userInput,
		d.formatRecentTurns(state),
		strings.Join(beatDescs, "\n"),
		strings.Join(availableRoles, ", "),
	)
}

// formatRecentTurns 格式化最近的对话轮次
func (d *DirectorEngine) formatRecentTurns(state *model.SessionState) string {
	if len(state.Turns) == 0 {
		return "(无历史对话)"
	}

	start := len(state.Turns) - 4
	if start < 0 {
		start = 0
	}

	lines := make([]string, 0)
	for _, turn := range state.Turns[start:] {
		lines = append(lines, fmt.Sprintf("  [%s]: %s", turn.Role, turn.Text))
	}

	return strings.Join(lines, "\n")
}

// initBeatLibrary 初始化拍点库
func initBeatLibrary() map[string]*BeatCard {
	return map[string]*BeatCard{
		"reveal": {
			BeatID:             "reveal",
			Goal:               "用简单比喻解释核心概念，降维打击",
			UserMustDoType:     "复述理解",
			TalkBurstLimitHint: 20,
			ExitCondition:      "用户能用自己的话复述比喻",
			NextSuggest:        []string{"check", "lens_shift"},
		},
		"check": {
			BeatID:             "check",
			Goal:               "快速检验用户理解，逼出输出",
			UserMustDoType:     "回答问题",
			TalkBurstLimitHint: 15,
			ExitCondition:      "用户给出明确答案",
			NextSuggest:        []string{"deepen", "twist", "continue"},
		},
		"deepen": {
			BeatID:             "deepen",
			Goal:               "深入机制链，引导更深层理解",
			UserMustDoType:     "阐述推理",
			TalkBurstLimitHint: 25,
			ExitCondition:      "用户能解释因果关系",
			NextSuggest:        []string{"check", "feynman"},
		},
		"twist": {
			BeatID:             "twist",
			Goal:               "用反例打破错觉，戳破误解",
			UserMustDoType:     "重新思考",
			TalkBurstLimitHint: 20,
			ExitCondition:      "用户意识到矛盾",
			NextSuggest:        []string{"reveal", "check"},
		},
		"continue": {
			BeatID:             "continue",
			Goal:               "保持叙事惯性，小步推进",
			UserMustDoType:     "跟随思路",
			TalkBurstLimitHint: 20,
			ExitCondition:      "自然过渡到下一话题",
			NextSuggest:        []string{"check", "deepen"},
		},
		"lens_shift": {
			BeatID:             "lens_shift",
			Goal:               "换视角重新解释，澄清边界",
			UserMustDoType:     "对比理解",
			TalkBurstLimitHint: 25,
			ExitCondition:      "用户能区分不同视角",
			NextSuggest:        []string{"check", "deepen"},
		},
		"feynman": {
			BeatID:             "feynman",
			Goal:               "让用户讲给别人听，巩固理解",
			UserMustDoType:     "教别人",
			TalkBurstLimitHint: 30,
			ExitCondition:      "用户能清晰地教给假想对象",
			NextSuggest:        []string{"montage", "exit_ticket"},
		},
		"montage": {
			BeatID:             "montage",
			Goal:               "快速切换多个场景，展示迁移",
			UserMustDoType:     "识别模式",
			TalkBurstLimitHint: 30,
			ExitCondition:      "用户能识别跨场景的共同模式",
			NextSuggest:        []string{"exit_ticket"},
		},
		"minigame": {
			BeatID:             "minigame",
			Goal:               "通过互动游戏降低负荷，恢复能量",
			UserMustDoType:     "参与互动",
			TalkBurstLimitHint: 20,
			ExitCondition:      "用户完成互动任务",
			NextSuggest:        []string{"continue", "exit_ticket"},
		},
		"exit_ticket": {
			BeatID:             "exit_ticket",
			Goal:               "最终测评，检验迁移能力",
			UserMustDoType:     "迁移应用",
			TalkBurstLimitHint: 15,
			ExitCondition:      "用户完成测评题",
			NextSuggest:        []string{},
		},
	}
}

// Utility functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsState(states []string, target string) bool {
	return contains(states, target)
}

func dedup(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
