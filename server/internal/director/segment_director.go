package director

import (
	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/llm"
	"bubble-talk/server/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// SegmentDirector 基于 Segment 的导演引擎
// 核心工作：读剧本 + 看现状 → 选角色 + 给 Segment 任务
type SegmentDirector struct {
	config    *config.DirectorConfig
	llmClient llm.Client

	// 可用的 Segment 类型
	segmentTypes []string

	// 脚本存储（简化实现，实际应该是数据库）
	scripts map[string]*model.Script
}

// NewSegmentDirector 创建基于 Segment 的导演引擎
func NewSegmentDirector(cfg *config.Config, llmClient llm.Client) *SegmentDirector {
	segmentTypes := []string{
		"ColdOpen",   // 开场冲突
		"Setup",      // 定义问题边界
		"DeepDive",   // 深入解释
		"Debate",     // 对抗澄清
		"Montage",    // 多场景迁移
		"MiniGame",   // 互动游戏
		"Wrap",       // 总结收束
		"HookBack",   // 拉回主线
		"ExitTicket", // 最终测评
	}

	return &SegmentDirector{
		config:       &cfg.Director,
		llmClient:    llmClient,
		segmentTypes: segmentTypes,
		scripts:      make(map[string]*model.Script),
	}
}

// DecideSegment 决定下一个 Segment
// 这是新版导演引擎的核心方法
func (d *SegmentDirector) DecideSegment(
	ctx context.Context,
	state *model.SessionState,
	userInput string,
) (*model.SegmentPlan, error) {

	// Step 1: 加载或获取剧本
	script, err := d.getOrLoadScript(state.EntryID)
	if err != nil {
		log.Printf("⚠️ Failed to load script: %v, will use fallback", err)
		script = nil
	}

	// Step 2: 计算对齐度（如果有剧本）
	alignmentScore := 0.5
	alignmentMode := "ADAPT"
	if script != nil && state.Script != nil {
		alignmentScore = d.calculateAlignment(ctx, script, state, userInput)
		alignmentMode = d.determineAlignmentMode(alignmentScore)
	}

	// Step 3: 判断是否需要更新剧本
	scriptRevision := d.shouldReviseScript(ctx, script, state, userInput, alignmentScore)
	if scriptRevision != nil && script != nil {
		// 更新剧本
		script.CurrentStory = scriptRevision.NewStory
		script.UpdatedAt = time.Now()

		// 记录修改历史
		if state.Script != nil {
			state.Script.Revisions = append(state.Script.Revisions, model.ScriptRevision{
				Timestamp: time.Now(),
				Reason:    scriptRevision.Reason,
				Change:    scriptRevision.Change,
			})
		}

		log.Printf("📝 Script revised: %s", scriptRevision.Reason)
	}

	// Step 4: 更新故事进度摘要
	storyProgress := d.summarizeStoryProgress(ctx, state)
	if state.Script != nil {
		state.Script.StoryProgress = storyProgress
		state.Script.AlignmentScore = alignmentScore
		state.Script.AlignmentMode = alignmentMode
		state.Script.LastAlignmentAt = time.Now()
	}

	// Step 5: 应用硬约束，生成候选
	candidates := d.generateSegmentCandidates(state, userInput)

	// Step 6: 让 LLM 决策：选角色 + 选 Segment 类型 + 生成任务
	segmentPlan, err := d.decideSegmentWithLLM(
		ctx,
		script,
		state,
		userInput,
		candidates,
		alignmentMode,
		storyProgress,
	)
	if err != nil {
		return nil, fmt.Errorf("LLM segment decision: %w", err)
	}

	// Step 7: 应用护栏验证
	segmentPlan = d.applySegmentGuardrails(segmentPlan, state)

	return segmentPlan, nil
}

// calculateAlignment 计算当前状态与剧本预期的对齐度
func (d *SegmentDirector) calculateAlignment(
	ctx context.Context,
	script *model.Script,
	state *model.SessionState,
	userInput string,
) float64 {
	// 简化实现：通过 LLM 评估对齐度
	// 实际可以结合规则（如检查关键情节是否已触发）

	systemPrompt := `你是一个剧本对齐度评估专家。
	
任务：评估当前对话状态与剧本预期的对齐度。

对齐度评分标准（0-1）：
- 0.9-1.0: 完全按剧本走，用户反应符合预期
- 0.7-0.9: 基本按剧本走，有小偏差
- 0.4-0.7: 主题一致但推进方式偏离
- 0.0-0.4: 用户需求/行为与剧本预期差异大

返回 JSON: {"score": 0.75, "reason": "..."}`

	userPrompt := fmt.Sprintf(`## 剧本故事

%s

## 已发生的故事

%s

## 用户最新输入

"%s"

## 用户状态

- 掌握度: %.2f
- 误解标签: %v
- 认知负荷: %d

请评估对齐度。`,
		script.CurrentStory,
		state.Script.StoryProgress,
		userInput,
		state.MasteryEstimate,
		state.MisconceptionTags,
		state.CognitiveLoad,
	)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	schema := &llm.JSONSchema{
		Name: "alignment_score",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"score": map[string]any{
					"type":        "number",
					"description": "对齐度评分 0-1",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "评分理由",
				},
			},
			"required":             []string{"score", "reason"},
			"additionalProperties": false,
		},
		Strict: true,
	}

	response, err := d.llmClient.Complete(ctx, messages, schema)
	if err != nil {
		log.Printf("⚠️ Alignment calculation failed: %v, using default 0.5", err)
		return 0.5
	}

	var result struct {
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		log.Printf("⚠️ Parse alignment result failed: %v", err)
		return 0.5
	}

	log.Printf("📊 Alignment: %.2f - %s", result.Score, result.Reason)
	return result.Score
}

// determineAlignmentMode 根据对齐度决定运行模式
func (d *SegmentDirector) determineAlignmentMode(score float64) string {
	if score > 0.7 {
		return "FOLLOW"
	} else if score > 0.4 {
		return "ADAPT"
	}
	return "REWRITE"
}

// ScriptRevisionResult 剧本修订结果
type ScriptRevisionResult struct {
	NewStory string
	Reason   string
	Change   string
}

// shouldReviseScript 判断是否需要修订剧本
func (d *SegmentDirector) shouldReviseScript(
	ctx context.Context,
	script *model.Script,
	state *model.SessionState,
	userInput string,
	alignmentScore float64,
) *ScriptRevisionResult {

	// 如果没有剧本，不需要修订
	if script == nil {
		return nil
	}

	// 规则：只有在严重偏离时才考虑修订剧本
	// 1. 对齐度 < 0.3（严重偏离）
	// 2. 用户提前触发了后续情节
	// 3. 用户强烈抗拒某个方向

	if alignmentScore >= 0.3 {
		// 对齐度还可以，不需要改剧本
		return nil
	}

	// 让 LLM 判断是否需要修订以及如何修订
	systemPrompt := `你是一个剧本修订专家。

任务：判断是否需要修订剧本，以及如何修订。

修订原则：
- 倾向于不改：只有在严重偏离时才改
- 用户提前触发某个情节 → 标记该情节已发生，避免重复
- 用户强烈抗拒某方向 → 调整后续走向
- 用户展现意外深度 → 跳过基础部分

返回 JSON:
{
  "should_revise": true/false,
  "new_story": "修订后的剧本（如果需要修订）",
  "reason": "为什么修订",
  "change": "改了什么（简短描述）"
}`

	userPrompt := fmt.Sprintf(`## 原始剧本

%s

## 当前剧本

%s

## 已发生的故事

%s

## 用户最新输入

"%s"

## 用户状态

- 掌握度: %.2f
- 误解标签: %v
- 对齐度: %.2f（严重偏离）

请判断是否需要修订剧本。`,
		script.OriginalStory,
		script.CurrentStory,
		state.Script.StoryProgress,
		userInput,
		state.MasteryEstimate,
		state.MisconceptionTags,
		alignmentScore,
	)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	schema := &llm.JSONSchema{
		Name: "script_revision",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"should_revise": map[string]any{
					"type":        "boolean",
					"description": "是否需要修订",
				},
				"new_story": map[string]any{
					"type":        "string",
					"description": "修订后的剧本",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "修订理由",
				},
				"change": map[string]any{
					"type":        "string",
					"description": "改动描述",
				},
			},
			"required":             []string{"should_revise", "new_story", "reason", "change"},
			"additionalProperties": false,
		},
		Strict: true,
	}

	response, err := d.llmClient.Complete(ctx, messages, schema)
	if err != nil {
		log.Printf("⚠️ Script revision check failed: %v", err)
		return nil
	}

	var result struct {
		ShouldRevise bool   `json:"should_revise"`
		NewStory     string `json:"new_story"`
		Reason       string `json:"reason"`
		Change       string `json:"change"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		log.Printf("⚠️ Parse revision result failed: %v", err)
		return nil
	}

	if !result.ShouldRevise {
		return nil
	}

	return &ScriptRevisionResult{
		NewStory: result.NewStory,
		Reason:   result.Reason,
		Change:   result.Change,
	}
}

// summarizeStoryProgress 总结已发生的故事
func (d *SegmentDirector) summarizeStoryProgress(
	ctx context.Context,
	state *model.SessionState,
) string {
	if len(state.Turns) == 0 {
		return "对话刚开始，尚未发生任何情节。"
	}

	// 取最近 20 轮对话（更多上下文）
	start := len(state.Turns) - 20
	if start < 0 {
		start = 0
	}

	recentTurns := state.Turns[start:]
	turnsText := make([]string, 0, len(recentTurns))
	for _, turn := range recentTurns {
		turnsText = append(turnsText, fmt.Sprintf("[%s]: %s", turn.Role, turn.Text))
	}

	systemPrompt := `你是一个故事摘要专家。

任务：总结已发生的故事，重点关注剧情推进和角色互动。

要求：
- 300-500字，详细但不冗余
- 记录关键剧情点：谁说了什么、产生了什么效果
- 记录用户的参与：用户说了什么、展现了什么理解/困惑
- 记录角色互动：角色之间如何配合、如何推进
- 记录悬念和待解决的问题
- **不要**总结成"教学进度"，而是"剧情进展"

格式：
【剧情进展】：谁做了什么、产生了什么效果
【用户参与】：用户的反应和理解状态
【当前状态】：故事推进到哪里、下一步可能去哪里
【待解决】：有哪些悬念或问题还没解决

返回纯文本，按上述格式组织。`

	userPrompt := fmt.Sprintf(`## 对话历史

%s

## 用户状态

- 掌握度: %.2f
- 误解标签: %v
- 认知负荷: %d/10

请总结已发生的故事。`,
		strings.Join(turnsText, "\n"),
		state.MasteryEstimate,
		state.MisconceptionTags,
		state.CognitiveLoad,
	)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	response, err := d.llmClient.Complete(ctx, messages, nil)
	if err != nil {
		log.Printf("⚠️ Story progress summary failed: %v", err)
		return "【剧情进展】：无法生成摘要\n【用户参与】：未知\n【当前状态】：未知\n【待解决】：未知"
	}

	return strings.TrimSpace(response)
}

// generateSegmentCandidates 生成候选 Segment 类型（应用硬约束）
func (d *SegmentDirector) generateSegmentCandidates(
	state *model.SessionState,
	userInput string,
) []string {
	candidates := make([]string, 0)

	// 硬约束 1: 用户明确要求结束
	if strings.Contains(strings.ToLower(userInput), "结束") ||
		strings.Contains(strings.ToLower(userInput), "退出") {
		return []string{"ExitTicket", "Wrap"}
	}

	// 硬约束 2: 长时间无有效输出，强制窗口
	if state.LastEffectiveOutputSec > 90 {
		return []string{"DeepDive", "Debate", "ExitTicket"}
	}

	// 硬约束 3: 疲惫状态
	if state.Signals.LastUserChars < 10 && state.Signals.LastUserLatencyMS > 5000 {
		return []string{"MiniGame", "Wrap", "ExitTicket"}
	}

	// 硬约束 4: 高认知负荷
	if state.CognitiveLoad > 7 {
		candidates = append(candidates, "HookBack", "MiniGame")
	}

	// 正常情况：所有类型都可选
	if len(candidates) == 0 {
		candidates = d.segmentTypes
	}

	return candidates
}

// decideSegmentWithLLM 使用 LLM 决策 Segment
// 核心：让 LLM 基于剧本、已发生的故事、用户交互，决定具体的剧情戏份和回应策略
func (d *SegmentDirector) decideSegmentWithLLM(
	ctx context.Context,
	script *model.Script,
	state *model.SessionState,
	userInput string,
	candidates []string,
	alignmentMode string,
	storyProgress string,
) (*model.SegmentPlan, error) {

	systemPrompt := d.buildSegmentSystemPromptV2()
	userPrompt := d.buildSegmentUserPromptV2(
		script,
		state,
		userInput,
		alignmentMode,
		storyProgress,
	)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// 简化的 JSON Schema：只要求核心字段
	schema := &llm.JSONSchema{
		Name: "segment_plan",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role_id": map[string]any{
					"type":        "string",
					"description": "选择哪个角色主导这一段戏",
				},
				"scene_direction": map[string]any{
					"type":        "string",
					"description": "具体的剧情戏份（导演分镜）：这个角色要说什么内容、用什么方式、达成什么效果、说完做什么。200-400字，详细描述这一段戏怎么演",
				},
				"response_approach": map[string]any{
					"type":        "string",
					"description": "如何回应用户（如有用户输入）：先做什么、再做什么、最后做什么。如果是角色互动或主动推进，说明'本段为角色对话'或'主动推进剧情'",
				},
				"user_must_do_type": map[string]any{
					"type":        "string",
					"description": "用户必须完成的输出类型：teach_back, choice, example, boundary, none",
				},
				"user_must_do_prompt": map[string]any{
					"type":        "string",
					"description": "给用户的具体提示（如果需要用户输出）",
				},
				"max_duration_sec": map[string]any{
					"type":        "integer",
					"description": "这段戏的最大时长（秒）",
				},
				"director_notes": map[string]any{
					"type":        "string",
					"description": "导演决策说明：为什么选这个角色、如何衔接上一段、为什么这样安排",
				},
			},
			"required": []string{
				"role_id", "scene_direction", "response_approach",
				"user_must_do_type", "user_must_do_prompt",
				"max_duration_sec", "director_notes",
			},
			"additionalProperties": false,
		},
		Strict: true,
	}

	response, err := d.llmClient.Complete(ctx, messages, schema)
	if err != nil {
		return nil, fmt.Errorf("LLM complete: %w", err)
	}

	var planData struct {
		RoleID           string   `json:"role_id"`
		SceneDirection   string   `json:"scene_direction"`
		UserIntent       string   `json:"user_intent"`
		UserMindState    []string `json:"user_mind_state"`
		ResponseApproach string   `json:"response_approach"`
		NeedUserOutput   bool     `json:"need_user_output"`
		NarrativeMode    string   `json:"narrative_mode"`
		NarrativeTone    string   `json:"narrative_tone"`
		TeachingGoal     string   `json:"teaching_goal"`
		UserMustDoType   string   `json:"user_must_do_type"`
		UserMustDoPrompt string   `json:"user_must_do_prompt"`
		MaxDurationSec   int      `json:"max_duration_sec"`
		ScriptReference  string   `json:"script_reference"`
		DirectorNotes    string   `json:"director_notes"`
	}

	if err := json.Unmarshal([]byte(response), &planData); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	// 构建 SegmentPlan
	segmentPlan := &model.SegmentPlan{
		SegmentID:      fmt.Sprintf("seg_%d", time.Now().Unix()),
		RoleID:         planData.RoleID,
		SceneDirection: planData.SceneDirection,
		NarrativeTilt: model.NarrativeTilt{
			Mode:          planData.NarrativeMode,
			Tone:          planData.NarrativeTone,
			TeachingStyle: "SOCRATIC",
		},
		SegmentGoal: model.SegmentGoal{
			Teaching: planData.TeachingGoal,
			UserMustDo: &model.UserMustDo{
				Type:   planData.UserMustDoType,
				Prompt: planData.UserMustDoPrompt,
			},
		},
		AutonomyBudget: model.AutonomyBudget{
			MaxSec:   planData.MaxDurationSec,
			MaxTurns: planData.MaxDurationSec / 10,
		},
		InteractionWindows: []model.InteractionWindow{
			{
				WindowID:   "w1",
				Trigger:    "BEFORE_WRAP",
				MaxWaitSec: 15,
				UserMustDo: &model.UserMustDo{
					Type:   planData.UserMustDoType,
					Prompt: planData.UserMustDoPrompt,
				},
			},
		},
		Guardrails: model.Guardrails{
			MaxTotalOutputSec: 120,
			MustReference:     state.MisconceptionTags,
			DisallowNewRoles:  true,
		},
		DirectorNotes:   planData.DirectorNotes,
		ScriptReference: planData.ScriptReference,
	}

	// 如果有用户输入，构建回应策略
	if userInput != "" && planData.ResponseApproach != "" {
		needHookBack := planData.UserIntent == "off_topic"
		segmentPlan.UserResponseStrategy = &model.UserResponseStrategy{
			UserIntent:       planData.UserIntent,
			UserMindState:    planData.UserMindState,
			ResponseApproach: planData.ResponseApproach,
			NeedUserOutput:   planData.NeedUserOutput,
			NeedHookBack:     needHookBack,
		}
	}

	return segmentPlan, nil
}

// applySegmentGuardrails 应用 Segment 护栏
func (d *SegmentDirector) applySegmentGuardrails(
	plan *model.SegmentPlan,
	state *model.SessionState,
) *model.SegmentPlan {
	// 验证 role_id
	if !contains(state.AvailableRoles, plan.RoleID) {
		log.Printf("⚠️ Invalid role '%s', fallback to first available", plan.RoleID)
		plan.RoleID = state.AvailableRoles[0]
	}

	// 验证 scene_direction 不为空
	if strings.TrimSpace(plan.SceneDirection) == "" {
		log.Printf("⚠️ Empty scene_direction, this should not happen")
		plan.SceneDirection = "继续对话，推进理解"
	}

	return plan
}

// buildSegmentSystemPromptV2 构建系统提示词（从文件加载）
func (d *SegmentDirector) buildSegmentSystemPromptV2() string {
	// 尝试从文件加载
	promptPath := "internal/director/prompts/segment_director_system.txt"
	content, err := os.ReadFile(promptPath)
	if err != nil {
		// 如果文件不存在，使用内嵌的简化版本
		log.Printf("⚠️ Failed to load prompt file: %v, using embedded prompt", err)
		return d.getEmbeddedSystemPrompt()
	}
	return string(content)
}

// getEmbeddedSystemPrompt 内嵌的系统提示词（备用）
func (d *SegmentDirector) getEmbeddedSystemPrompt() string {
	return `你是一个专业的对话节目导演。

你在拍一档对话节目，不是课堂。用户是参与者，但不总是主角。
有时候角色之间对话就能推进剧情，用户在关键时刻参与。

核心工作：决定下一段戏怎么演
1. 选角：谁来主导下一段
2. 写分镜：200-400字详细描述这段戏怎么演
3. 定节奏：这段多长、要达到什么效果

三种场景：
A. 有用户刚说话 - 可能需要回应（判断用户意图和心理状态，选择合适的 beat 策略）
B. 没有用户输入 - 角色互动或主动推进
C. 角色间互动 - 用户旁听，关键时刻参与

scene_direction 必须包含：
- 具体说什么内容
- 用什么方式说
- 要达成什么效果
- 说完后做什么

关键原则：
1. 剧情连贯 > 单段完美（考虑上一段的出口）
2. 节目感 > 说教感（讲好故事，不是上课）
3. 用户参与要巧不要频（不是每段都要用户说话）
4. response_approach 只在需要回应用户时填写
5. 必须衔接上一段（director_notes 说明如何衔接）

严格按 JSON Schema 返回。`
}

// getRoleDescription 动态获取角色描述（不写死在提示词里）
func getRoleDescription(roleID string) string {
	descriptions := map[string]string{
		"host":      "控节奏、抛冲突、引导对话、用通俗语言翻译专业内容",
		"economist": "解释机制、给框架、严谨但不端着、可和主持人辩论",
		"skeptic":   "提反例、挑战假设、制造张力、代表普通人直觉",
		"expert":    "深度解释、给专业视角、澄清复杂概念",
		"narrator":  "讲故事、制造场景、用生动语言描述",
	}
	if desc, ok := descriptions[roleID]; ok {
		return desc
	}
	return "参与对话、推进剧情"
}

// buildSegmentUserPromptV2 构建用户提示词（V2：动态拼接，考虑 beat 策略）
func (d *SegmentDirector) buildSegmentUserPromptV2(
	script *model.Script,
	state *model.SessionState,
	userInput string,
	alignmentMode string,
	storyProgress string,
) string {
	// 剧本部分
	scriptStory := "(无剧本，完全基于用户状态即兴)"
	if script != nil {
		scriptStory = script.CurrentStory
	}

	// 故事进度部分 - 条件性显示（>= 5轮对话才显示）
	storyProgressSection := ""
	if len(state.Turns) >= 5 {
		storyProgressSection = fmt.Sprintf("## 已发生的故事\n\n%s\n\n---\n\n", storyProgress)
	}

	// 用户交互部分 - 动态构建
	userInteractionSection := ""
	if userInput != "" {
		userInteractionSection = fmt.Sprintf(`## 用户刚说了什么

"%s"

**你需要判断**：
1. 用户意图：提问？挑战？补充？跑题？困惑？
2. 用户心理状态：迷雾？半懂？顿悟？疲惫？
3. 基于用户状态选择 beat 策略：
   - 困惑(Fog) → 简单比喻澄清
   - 半懂(Partial) → 深入解释或换视角
   - 顿悟(Aha) → 让TA复述/教别人
   - 疲惫(Fatigue) → 降低负荷或互动

---

`, userInput)
	} else {
		userInteractionSection = `## 当前状态

没有用户输入 - 推进剧情的时机
- 角色对话（用户旁听）
- 向用户抛窗口
- 继续推进剧情

---

`
	}

	// 上一段信息 - 保证连贯性
	lastSegmentInfo := ""
	if state.CurrentSegment != nil {
		lastSegmentInfo = fmt.Sprintf(`## 上一段的"出口"

重要：考虑上一段如何结束
- 若在等用户反应，必须延续
- 若要切角色，需合理过渡
- 不能突然切换

---

`)
	}

	// 动态构建角色列表（不写死）
	rolesList := "## 可用角色\n\n"
	for _, roleID := range state.AvailableRoles {
		roleDesc := getRoleDescription(roleID)
		rolesList += fmt.Sprintf("- **%s**: %s\n", roleID, roleDesc)
	}

	return fmt.Sprintf(`## 剧本

%s

## 对齐模式：%s

---

%s## 用户状态

- 掌握度: %.2f/1.0
- 误解: %v
- 认知负荷: %d/10
- 紧张度: %d/10
- 最近输出: %d字符

---

%s%s%s## 最近对话

%s

---

%s---

## 任务

1. 选角（role_id）
2. 写分镜（200-400字）
3. 回应方式（response_approach）
4. 用户输出要求
5. 时长、决策说明

严格按 JSON Schema 返回。`,
		scriptStory,
		alignmentMode,
		storyProgressSection,
		state.MasteryEstimate,
		state.MisconceptionTags,
		state.CognitiveLoad,
		state.TensionLevel,
		state.Signals.LastUserChars,
		userInteractionSection,
		lastSegmentInfo,
		d.formatRecentTurns(state, 4),
		rolesList,
	)
}

// getOrLoadScript 获取或加载剧本
func (d *SegmentDirector) getOrLoadScript(entryID string) (*model.Script, error) {
	// 简化实现：从内存缓存获取
	// 实际应该从数据库/文件加载
	scriptID := "script_" + entryID
	if script, ok := d.scripts[scriptID]; ok {
		return script, nil
	}

	// TODO: 从数据库或文件加载剧本
	// 这里返回一个示例剧本
	script := &model.Script{
		ScriptID:      scriptID,
		EntryID:       entryID,
		OriginalStory: d.getDefaultScript(entryID),
		CurrentStory:  d.getDefaultScript(entryID),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Version:       "1.0",
	}

	d.scripts[scriptID] = script
	return script, nil
}

// getDefaultScript 获取默认剧本（示例）
func (d *SegmentDirector) getDefaultScript(entryID string) string {
	// 这里应该根据 entryID 返回对应的剧本
	// 简化实现，返回通用模板
	return `# 对话节目剧本模板

## 主题
通过对话式教学，让用户深入理解核心概念。

## 开场（ColdOpen）
- 用一个生活化的场景或冲突抛出问题
- 制造好奇：让用户想知道"为什么"

## 推进（DeepDive / Debate）
- 主持人引导，专家解释
- 通过对话逐步澄清概念
- 在关键点让用户参与（选择、复述、举例）

## 收束（Wrap）
- 总结核心观点
- 给出迁移建议
- 测评用户理解

## 风格
- 轻松但不失严谨
- 像访谈节目，不是课堂讲座
- 用户是嘉宾/观众，不是答题机`
}

// formatRecentTurns 格式化最近的对话轮次
func (d *SegmentDirector) formatRecentTurns(state *model.SessionState, count int) string {
	if len(state.Turns) == 0 {
		return "(无历史对话)"
	}

	start := len(state.Turns) - count
	if start < 0 {
		start = 0
	}

	lines := make([]string, 0)
	for _, turn := range state.Turns[start:] {
		lines = append(lines, fmt.Sprintf("[%s]: %s", turn.Role, turn.Text))
	}

	return strings.Join(lines, "\n")
}
