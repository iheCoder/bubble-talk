package actor

import (
	"bubble-talk/server/internal/model"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ActorEngine 负责根据导演计划构建 Prompt
type ActorEngine struct {
	promptsDir  string
	rolePrompts map[string]string
	beatPrompts map[string]string
}

// ActorRequest 演员引擎的输入请求
type ActorRequest struct {
	SessionID     string
	TurnID        string
	Plan          model.DirectorPlan
	EntryID       string
	Domain        string
	MainObjective string
	ConceptName   string
	LastUserText  string
	Metaphor      string
}

// ActorPrompt 演员引擎的输出
type ActorPrompt struct {
	Instructions string
	DebugInfo    map[string]interface{}
}

// NewActorEngine 创建演员引擎
func NewActorEngine(promptsDir string) (*ActorEngine, error) {
	if promptsDir == "" {
		promptsDir = "configs/prompts"
	}
	engine := &ActorEngine{
		promptsDir:  promptsDir,
		rolePrompts: make(map[string]string),
		beatPrompts: make(map[string]string),
	}
	if err := engine.loadPrompts(); err != nil {
		return nil, fmt.Errorf("failed to load prompts: %w", err)
	}
	return engine, nil
}

// loadPrompts 加载所有 Prompt 模板
func (a *ActorEngine) loadPrompts() error {
	rolesDir := filepath.Join(a.promptsDir, "roles")
	roleFiles, err := os.ReadDir(rolesDir)
	if err != nil {
		return fmt.Errorf("read roles dir: %w", err)
	}
	for _, file := range roleFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
			continue
		}
		roleName := strings.TrimSuffix(file.Name(), ".md")
		content, err := os.ReadFile(filepath.Join(rolesDir, file.Name()))
		if err != nil {
			return fmt.Errorf("read role %s: %w", roleName, err)
		}
		a.rolePrompts[roleName] = string(content)
	}

	beatsDir := filepath.Join(a.promptsDir, "beats")
	beatFiles, err := os.ReadDir(beatsDir)
	if err != nil {
		return fmt.Errorf("read beats dir: %w", err)
	}
	for _, file := range beatFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
			continue
		}
		beatName := strings.TrimSuffix(file.Name(), ".md")
		content, err := os.ReadFile(filepath.Join(beatsDir, file.Name()))
		if err != nil {
			return fmt.Errorf("read beat %s: %w", beatName, err)
		}
		a.beatPrompts[beatName] = string(content)
	}
	return nil
}

// BuildPrompt 根据 ActorRequest 构建完整的 Prompt
func (a *ActorEngine) BuildPrompt(req ActorRequest) (ActorPrompt, error) {
	rolePrompt, ok := a.rolePrompts[req.Plan.NextRole]
	if !ok {
		return ActorPrompt{}, fmt.Errorf("role not found: %s", req.Plan.NextRole)
	}
	beatPrompt, ok := a.beatPrompts[req.Plan.NextBeat]
	if !ok {
		return ActorPrompt{}, fmt.Errorf("beat not found: %s", req.Plan.NextBeat)
	}

	instructions := a.assembleInstructions(req, rolePrompt, beatPrompt)
	debugInfo := map[string]interface{}{
		"session_id": req.SessionID, "turn_id": req.TurnID,
		"role": req.Plan.NextRole, "beat": req.Plan.NextBeat,
		"output_action":    req.Plan.OutputAction,
		"talk_burst_limit": req.Plan.TalkBurstLimitSec,
		"user_mind_state":  req.Plan.UserMindState,
	}

	return ActorPrompt{Instructions: instructions, DebugInfo: debugInfo}, nil
}

// assembleInstructions 组装完整的指令文本
func (a *ActorEngine) assembleInstructions(req ActorRequest, rolePrompt, beatPrompt string) string {
	var sb strings.Builder

	sb.WriteString("[Role Definition]\n")
	sb.WriteString(a.extractRoleEssence(rolePrompt))
	sb.WriteString("\n\n")

	sb.WriteString("[Current Situation]\n")
	sb.WriteString(fmt.Sprintf("User Mind State: %s\n", strings.Join(req.Plan.UserMindState, ", ")))
	sb.WriteString(fmt.Sprintf("User Intent: %s\n", req.Plan.Intent))
	if req.LastUserText != "" {
		sb.WriteString(fmt.Sprintf("Last User Input: \"%s\"\n", req.LastUserText))
	}
	sb.WriteString(fmt.Sprintf("Main Learning Objective: %s\n", req.MainObjective))
	sb.WriteString("\n")

	sb.WriteString("[Strategy & Task]\n")
	sb.WriteString(fmt.Sprintf("Beat: %s\n", req.Plan.NextBeat))
	sb.WriteString(fmt.Sprintf("Output Action: %s\n", req.Plan.OutputAction))
	sb.WriteString("\n")
	sb.WriteString(a.extractBeatInstructions(beatPrompt, req))
	sb.WriteString("\n\n")

	sb.WriteString("[Constraints]\n")
	sb.WriteString(fmt.Sprintf("- Keep your response under %d seconds when spoken aloud.\n", req.Plan.TalkBurstLimitSec))
	sb.WriteString("- Use short, spoken-style sentences with natural pauses.\n")
	sb.WriteString("- Always end with a clear prompt for the user to respond.\n")
	sb.WriteString("- Speak in a conversational, natural tone as if talking to a friend.\n")

	if req.Plan.TensionGoal == "decrease" {
		sb.WriteString("- Keep the tone relaxed and encouraging.\n")
	}
	if req.Plan.LoadGoal == "decrease" {
		sb.WriteString("- Simplify your explanation. Avoid complex terminology.\n")
	}

	return sb.String()
}

// extractRoleEssence 从角色 Prompt 中提取核心人设
func (a *ActorEngine) extractRoleEssence(rolePrompt string) string {
	lines := strings.Split(rolePrompt, "\n")
	var essence strings.Builder
	inProfile := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "## Profile") {
			inProfile = true
			continue
		}
		if inProfile {
			if strings.HasPrefix(line, "##") {
				break
			}
			if line != "" && !strings.HasPrefix(line, "#") {
				essence.WriteString(line)
				essence.WriteString("\n")
			}
		}
	}
	if essence.Len() == 0 {
		for i, line := range lines {
			if i >= 5 {
				break
			}
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				essence.WriteString(line)
				essence.WriteString("\n")
			}
		}
	}
	return essence.String()
}

// extractBeatInstructions 从 Beat Prompt 中提取具体指令
func (a *ActorEngine) extractBeatInstructions(beatPrompt string, req ActorRequest) string {
	lines := strings.Split(beatPrompt, "\n")
	var instructions strings.Builder
	inTemplate, inCodeBlock := false, false

	for _, line := range lines {
		if strings.Contains(line, "## Prompt Template") {
			inTemplate = true
			continue
		}
		if inTemplate {
			if strings.HasPrefix(strings.TrimSpace(line), "##") {
				break
			}
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock {
				line = strings.ReplaceAll(line, "{concept}", req.ConceptName)
				line = strings.ReplaceAll(line, "{metaphor}", req.Metaphor)
				instructions.WriteString(line)
				instructions.WriteString("\n")
			}
		}
	}

	if instructions.Len() == 0 {
		instructions.WriteString(fmt.Sprintf("Execute the '%s' strategy.\n", req.Plan.NextBeat))
		instructions.WriteString(fmt.Sprintf("Action: %s\n", req.Plan.OutputAction))
	}
	return instructions.String()
}

// Validate 校验生成的 Prompt
func (a *ActorEngine) Validate(prompt ActorPrompt) error {
	if len(prompt.Instructions) == 0 {
		return fmt.Errorf("empty instructions")
	}
	requiredSections := []string{
		"[Role Definition]",
		"[Current Situation]",
		"[Strategy & Task]",
		"[Constraints]",
	}
	for _, section := range requiredSections {
		if !strings.Contains(prompt.Instructions, section) {
			return fmt.Errorf("missing required section: %s", section)
		}
	}
	if len(prompt.Instructions) > 2000 {
		return fmt.Errorf("instructions too long: %d > 2000", len(prompt.Instructions))
	}
	return nil
}

// BuildFallbackPrompt 构建兜底 Prompt
func (a *ActorEngine) BuildFallbackPrompt(req ActorRequest) ActorPrompt {
	instructions := fmt.Sprintf(`[Role Definition]
You are a helpful tutor.

[Current Situation]
The user needs help understanding: %s

[Strategy & Task]
Explain the concept simply and clearly.
Ask if the user has any questions.

[Constraints]
- Keep your response under %d seconds.
- Use simple, everyday language.
- End with a question to check understanding.
`, req.MainObjective, req.Plan.TalkBurstLimitSec)

	return ActorPrompt{
		Instructions: instructions,
		DebugInfo: map[string]interface{}{
			"fallback": true,
			"reason":   "Failed to build normal prompt",
		},
	}
}
