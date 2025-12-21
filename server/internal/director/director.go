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

	// Layer A: 硬约束校验（Deterministic Guardrails）
	flowMode := d.inferFlowMode(state, userInput)
	userMindState := d.inferUserMindState(state, userInput)

	// 生成候选拍点
	beatCandidates := d.generateBeatCandidates(state, flowMode, userMindState)

	var plan model.DirectorPlan

	// 如果启用 LLM，使用 LLM 进行拍点选择和选角
	if d.config.EnableLLM && d.llmClient != nil {
		llmPlan, err := d.decideLLM(ctx, state, userInput, flowMode, userMindState, beatCandidates)
		if err != nil {
			log.Printf("⚠️ LLM decision failed, falling back to rules: %v", err)
			plan = d.decideWithRules(state, userInput, flowMode, userMindState, beatCandidates)
		} else {
			plan = llmPlan
		}
	} else {
		// 使用规则引擎
		plan = d.decideWithRules(state, userInput, flowMode, userMindState, beatCandidates)
	}

	// Layer A: 应用硬约束（最后验证）
	plan = d.applyGuardrails(plan, state)

	return plan
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
func (d *DirectorEngine) decideLLM(
	ctx context.Context,
	state *model.SessionState,
	userInput string,
	flowMode string,
	userMindState []string,
	beatCandidates []string,
) (model.DirectorPlan, error) {
	// 构建提示词
	systemPrompt := d.buildSystemPrompt()
	userPrompt := d.buildUserPrompt(state, userInput, flowMode, userMindState, beatCandidates)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// 定义 JSON Schema
	schema := &llm.JSONSchema{
		Name: "director_plan",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"flow_mode":       map[string]any{"type": "string", "enum": []string{"FLOW", "RESCUE"}},
				"user_mind_state": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"intent":          map[string]any{"type": "string"},
				"next_beat":       map[string]any{"type": "string"},
				"next_role":       map[string]any{"type": "string"},
				"output_action":   map[string]any{"type": "string"},
				"user_must_do": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":   map[string]any{"type": "string"},
						"prompt": map[string]any{"type": "string"},
					},
					"required": []string{"type", "prompt"},
				},
				"talk_burst_limit_sec": map[string]any{"type": "integer"},
				"notes":                map[string]any{"type": "string"},
				"debug": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"beat_candidates":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"beat_choice_reason": map[string]any{"type": "string"},
						"role_choice_reason": map[string]any{"type": "string"},
					},
				},
			},
			"required": []string{"flow_mode", "user_mind_state", "intent", "next_beat", "next_role", "output_action", "talk_burst_limit_sec", "notes"},
		},
		Strict: true,
	}

	// 调用 LLM
	response, err := d.llmClient.Complete(ctx, messages, schema)
	if err != nil {
		return model.DirectorPlan{}, fmt.Errorf("LLM complete: %w", err)
	}

	// 解析响应
	var planData struct {
		FlowMode          string               `json:"flow_mode"`
		UserMindState     []string             `json:"user_mind_state"`
		Intent            string               `json:"intent"`
		NextBeat          string               `json:"next_beat"`
		NextRole          string               `json:"next_role"`
		OutputAction      string               `json:"output_action"`
		UserMustDo        *model.UserMustDo    `json:"user_must_do"`
		TalkBurstLimitSec int                  `json:"talk_burst_limit_sec"`
		Notes             string               `json:"notes"`
		Debug             *model.DirectorDebug `json:"debug"`
	}

	if err := json.Unmarshal([]byte(response), &planData); err != nil {
		return model.DirectorPlan{}, fmt.Errorf("unmarshal LLM response: %w", err)
	}

	// 构建 DirectorPlan
	plan := model.DirectorPlan{
		FlowMode:          planData.FlowMode,
		UserMindState:     planData.UserMindState,
		Intent:            planData.Intent,
		NextBeat:          planData.NextBeat,
		NextRole:          planData.NextRole,
		OutputAction:      planData.OutputAction,
		UserMustDo:        planData.UserMustDo,
		TalkBurstLimitSec: planData.TalkBurstLimitSec,
		TensionGoal:       d.determineTensionGoal(state),
		LoadGoal:          d.determineLoadGoal(state),
		StackAction:       "maintain",
		Notes:             planData.Notes,
		Debug:             planData.Debug,
	}

	return plan, nil
}

// decideWithRules 使用规则引擎进行决策
func (d *DirectorEngine) decideWithRules(
	state *model.SessionState,
	_ string,
	flowMode string,
	userMindState []string,
	beatCandidates []string,
) model.DirectorPlan {
	// 从候选中选择第一个（简单策略）
	nextBeat := beatCandidates[0]

	// 选择角色（轮换）
	nextRole := d.selectNextRole(state)

	// 确定输出动作
	outputAction := d.determineOutputAction(nextBeat)

	return model.DirectorPlan{
		FlowMode:          flowMode,
		UserMindState:     userMindState,
		Intent:            "clarify",
		NextBeat:          nextBeat,
		NextRole:          nextRole,
		OutputAction:      outputAction,
		TalkBurstLimitSec: d.determineTalkBurstLimit(state),
		TensionGoal:       d.determineTensionGoal(state),
		LoadGoal:          d.determineLoadGoal(state),
		StackAction:       "maintain",
		Notes:             "规则引擎选择",
		Debug: &model.DirectorDebug{
			BeatCandidates:   beatCandidates,
			BeatChoiceReason: "规则：选择第一个候选拍点",
		},
	}
}

// applyGuardrails 应用硬约束
func (d *DirectorEngine) applyGuardrails(plan model.DirectorPlan, state *model.SessionState) model.DirectorPlan {
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
	return `你是一个专业的对话导演（Director），负责决定下一个拍点（Beat）和角色（Role）。

你的职责：
1. 根据用户的心理状态选择最合适的拍点
2. 在泡泡固定角色集合中选择最适合的角色
3. 明确用户必须完成的输出动作
4. 控制对话节奏和认知负荷

关键原则：
- 不写台词，只做决策
- 选择必须在候选集合内
- 确保用户有可执行的输出要求
- 保持结构化输出

输出格式要求：
- 严格按照 JSON Schema 返回
- 所有字段必须填写
- debug 字段用于工程调试，必须写清选择理由`
}

// buildUserPrompt 构建用户提示词
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
