package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// QuizTool 选择题工具
// 由AI角色通过function calling调用来向用户展示选择题
// 题目内容由LLM根据对话上下文动态生成
type QuizTool struct {
	// 待处理的quiz队列（tool call产生的quiz会先存这里）
	pendingQuizzes []QuizData
	mu             sync.RWMutex

	// 回调函数：当工具被调用时，通知网关发送quiz到前端
	onQuizCreated func(quiz QuizData)
}

// QuizData 选择题数据
type QuizData struct {
	QuizID   string   `json:"quiz_id"`  // 题目ID
	Question string   `json:"question"` // 题目文本
	Options  []string `json:"options"`  // 选项列表
	Context  string   `json:"context"`  // 上下文（可选）
}

// NewQuizTool 创建选择题工具
func NewQuizTool(onQuizCreated func(QuizData)) *QuizTool {
	return &QuizTool{
		pendingQuizzes: make([]QuizData, 0),
		onQuizCreated:  onQuizCreated,
	}
}

// GetDefinition 返回工具定义
func (q *QuizTool) GetDefinition() ToolDefinition {
	return ToolDefinition{
		Type:        "function",
		Name:        "show_quiz",
		Description: "向用户展示一道选择题，用于检验理解或推动对话。只有在导演判断需要测评、检验或让用户做选择时才调用。",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"quiz_id": map[string]interface{}{
					"type":        "string",
					"description": "题目的唯一标识",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "题目文本，用口语化的方式提出问题",
				},
				"options": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "选项列表，每个选项是一个字符串",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "题目的上下文说明（可选）",
				},
			},
			"required": []string{"quiz_id", "question", "options"},
		},
	}
}

// Execute 执行工具调用
func (q *QuizTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 解析参数
	quizID, ok := args["quiz_id"].(string)
	if !ok || quizID == "" {
		return "", fmt.Errorf("missing or invalid quiz_id")
	}

	question, ok := args["question"].(string)
	if !ok || question == "" {
		return "", fmt.Errorf("missing or invalid question")
	}

	optionsRaw, ok := args["options"].([]interface{})
	if !ok || len(optionsRaw) == 0 {
		return "", fmt.Errorf("missing or invalid options")
	}

	options := make([]string, len(optionsRaw))
	for i, opt := range optionsRaw {
		optStr, ok := opt.(string)
		if !ok {
			return "", fmt.Errorf("invalid option at index %d", i)
		}
		options[i] = optStr
	}

	context, _ := args["context"].(string)

	// 创建quiz数据
	quiz := QuizData{
		QuizID:   quizID,
		Question: question,
		Options:  options,
		Context:  context,
	}

	// 保存到待处理队列
	q.mu.Lock()
	q.pendingQuizzes = append(q.pendingQuizzes, quiz)
	q.mu.Unlock()

	// 触发回调（通知网关发送到前端）
	if q.onQuizCreated != nil {
		q.onQuizCreated(quiz)
	}

	// 返回成功消息（这会作为function_call_output返回给模型）
	result := map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("题目已展示给用户，等待用户选择。题目ID: %s", quizID),
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

// GetPendingQuizzes 获取所有待处理的quiz
func (q *QuizTool) GetPendingQuizzes() []QuizData {
	q.mu.RLock()
	defer q.mu.RUnlock()

	quizzes := make([]QuizData, len(q.pendingQuizzes))
	copy(quizzes, q.pendingQuizzes)
	return quizzes
}

// ClearPendingQuizzes 清空待处理队列
func (q *QuizTool) ClearPendingQuizzes() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pendingQuizzes = q.pendingQuizzes[:0]
}
