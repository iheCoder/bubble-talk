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
	return nil
}

// BuildPrompt 根据 ActorRequest 构建完整的 Prompt
func (a *ActorEngine) BuildPrompt(req ActorRequest) (ActorPrompt, error) {
	rolePrompt, ok := a.rolePrompts[req.Plan.NextRole]
	if !ok {
		return ActorPrompt{}, fmt.Errorf("role not found: %s", req.Plan.NextRole)
	}
	if req.Plan.Instruction == "" {
		return ActorPrompt{}, fmt.Errorf("empty director instruction")
	}

	instructions := a.assembleInstructions(req, rolePrompt)
	debugInfo := map[string]interface{}{
		"session_id": req.SessionID, "turn_id": req.TurnID,
		"role":   req.Plan.NextRole,
		"debug":  req.Plan.Debug,
		"source": "director_plan",
	}

	return ActorPrompt{Instructions: instructions, DebugInfo: debugInfo}, nil
}

// assembleInstructions 组装完整的指令文本
func (a *ActorEngine) assembleInstructions(req ActorRequest, rolePrompt string) string {
	var sb strings.Builder

	sb.WriteString("[Role Definition]\n")
	sb.WriteString(a.extractRoleEssence(rolePrompt))
	sb.WriteString("\n\n")

	sb.WriteString("[Context]\n")
	if req.LastUserText != "" {
		sb.WriteString(fmt.Sprintf("Last User Input: \"%s\"\n", req.LastUserText))
	}
	if req.MainObjective != "" {
		sb.WriteString(fmt.Sprintf("Main Learning Objective: %s\n", req.MainObjective))
	}
	if req.ConceptName != "" {
		sb.WriteString(fmt.Sprintf("Concept Name: %s\n", req.ConceptName))
	}
	if req.Metaphor != "" {
		sb.WriteString(fmt.Sprintf("Metaphor Hint: %s\n", req.Metaphor))
	}
	sb.WriteString("\n")

	sb.WriteString("[Director Instructions]\n")
	sb.WriteString(req.Plan.Instruction)
	if !strings.HasSuffix(req.Plan.Instruction, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	sb.WriteString("[Constraints]\n")
	sb.WriteString("- Use short, spoken-style sentences with natural pauses.\n")
	sb.WriteString("- Speak in a conversational, natural tone as if talking to a friend.\n")

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

// Validate 校验生成的 Prompt
func (a *ActorEngine) Validate(prompt ActorPrompt) error {
	if len(prompt.Instructions) == 0 {
		return fmt.Errorf("empty instructions")
	}
	requiredSections := []string{
		"[Role Definition]",
		"[Context]",
		"[Director Instructions]",
		"[Constraints]",
	}
	for _, section := range requiredSections {
		if !strings.Contains(prompt.Instructions, section) {
			return fmt.Errorf("missing required section: %s", section)
		}
	}
	if len(prompt.Instructions) > 10000 {
		return fmt.Errorf("instructions too long: %d > 10000", len(prompt.Instructions))
	}
	return nil
}

// BuildFallbackPrompt 构建兜底 Prompt
func (a *ActorEngine) BuildFallbackPrompt(req ActorRequest) ActorPrompt {
	instructions := fmt.Sprintf(`[Role Definition]
You are a helpful tutor.

[Context]
The user needs help understanding: %s

[Director Instructions]
Explain the concept simply and clearly.
Answer if the user has any questions.

[Constraints]
- Use simple, everyday language.
- End with a question to check understanding.
`, req.MainObjective)

	return ActorPrompt{
		Instructions: instructions,
		DebugInfo: map[string]interface{}{
			"fallback": true,
			"reason":   "Failed to build normal prompt",
		},
	}
}
