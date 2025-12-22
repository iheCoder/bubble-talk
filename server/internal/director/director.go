package director

import (
	"strings"

	"bubble-talk/server/internal/config"
	"bubble-talk/server/internal/llm"
	"bubble-talk/server/internal/model"
)

// Director 是导演模块对外暴露的最小能力接口。
// 仅负责给出角色与导演指令，不关心 Actor 如何执行。
type Director interface {
	Decide(state *model.SessionState, userInput string) model.DirectorPlan
}

// NewDirector 根据配置选择导演实现。
// 默认返回经典 DirectorEngine，避免影响现有行为。
func NewDirector(cfg *config.Config, llmClient llm.Client) Director {
	directorType := strings.ToLower(strings.TrimSpace(cfg.Director.Type))
	switch directorType {
	case "beats":
		return NewDirectorEngine(cfg, llmClient)
	default:
		return NewSegmentDirector(cfg, llmClient)
	}
}
