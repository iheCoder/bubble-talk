package tool

import (
	"context"
	"encoding/json"
)

// ToolDefinition 定义工具的元数据（OpenAI Realtime API格式）
// 直接使用扁平结构而不是嵌套function字段
type ToolDefinition struct {
	Type        string                 `json:"type"`        // "function"
	Name        string                 `json:"name"`        // 工具名称
	Description string                 `json:"description"` // 工具描述
	Parameters  map[string]interface{} `json:"parameters"`  // JSON Schema格式的参数定义
}

// FunctionDefinition 函数定义（保留用于向后兼容）
// 已废弃：新代码应该直接使用ToolDefinition
type FunctionDefinition struct {
	Name        string                 `json:"name"`        // 函数名称
	Description string                 `json:"description"` // 函数描述
	Parameters  map[string]interface{} `json:"parameters"`  // JSON Schema格式的参数定义
}

// ToolExecutor 工具执行器接口
type ToolExecutor interface {
	// GetDefinition 返回工具定义（用于注册到OpenAI Realtime）
	GetDefinition() ToolDefinition

	// Execute 执行工具调用
	// 返回结果字符串和错误
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolRegistry 工具注册表
type ToolRegistry struct {
	tools map[string]ToolExecutor
}

// NewToolRegistry 创建工具注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolExecutor),
	}
}

// Register 注册工具
func (r *ToolRegistry) Register(executor ToolExecutor) {
	def := executor.GetDefinition()
	// 使用name字段而不是嵌套的function.name
	r.tools[def.Name] = executor
}

// Get 获取工具执行器
func (r *ToolRegistry) Get(name string) (ToolExecutor, bool) {
	executor, ok := r.tools[name]
	return executor, ok
}

// GetAllDefinitions 获取所有工具定义（用于session.update）
func (r *ToolRegistry) GetAllDefinitions() []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(r.tools))
	for _, executor := range r.tools {
		definitions = append(definitions, executor.GetDefinition())
	}
	return definitions
}

// Execute 执行工具调用
func (r *ToolRegistry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	executor, ok := r.Get(name)
	if !ok {
		return "", &ToolNotFoundError{ToolName: name}
	}

	// 解析参数
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", &InvalidArgsError{ToolName: name, Err: err}
	}

	// 执行工具
	return executor.Execute(ctx, args)
}

// ToolNotFoundError 工具未找到错误
type ToolNotFoundError struct {
	ToolName string
}

func (e *ToolNotFoundError) Error() string {
	return "tool not found: " + e.ToolName
}

// InvalidArgsError 无效参数错误
type InvalidArgsError struct {
	ToolName string
	Err      error
}

func (e *InvalidArgsError) Error() string {
	return "invalid args for tool " + e.ToolName + ": " + e.Err.Error()
}
