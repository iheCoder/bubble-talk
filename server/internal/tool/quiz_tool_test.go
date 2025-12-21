package tool

import (
	"context"
	"testing"
)

func TestQuizTool_GetDefinition(t *testing.T) {
	tool := NewQuizTool(nil)
	def := tool.GetDefinition()

	if def.Type != "function" {
		t.Errorf("Expected type 'function', got '%s'", def.Type)
	}

	if def.Name != "show_quiz" {
		t.Errorf("Expected name 'show_quiz', got '%s'", def.Name)
	}

	if def.Function.Name != "show_quiz" {
		t.Errorf("Expected function name 'show_quiz', got '%s'", def.Function.Name)
	}

	// 检查参数定义
	params, ok := def.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Parameters properties should be a map")
	}

	// 检查必填字段
	required, ok := def.Function.Parameters["required"].([]string)
	if !ok {
		t.Fatal("Required fields should be a string array")
	}

	expectedRequired := []string{"quiz_id", "question", "options"}
	if len(required) != len(expectedRequired) {
		t.Errorf("Expected %d required fields, got %d", len(expectedRequired), len(required))
	}

	// 检查所有必填字段都在
	for _, field := range expectedRequired {
		found := false
		for _, r := range required {
			if r == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Required field '%s' not found", field)
		}
	}

	// 检查参数属性
	if _, ok := params["quiz_id"]; !ok {
		t.Error("quiz_id parameter not found")
	}
	if _, ok := params["question"]; !ok {
		t.Error("question parameter not found")
	}
	if _, ok := params["options"]; !ok {
		t.Error("options parameter not found")
	}
	if _, ok := params["context"]; !ok {
		t.Error("context parameter not found")
	}
}

func TestQuizTool_Execute(t *testing.T) {
	var receivedQuiz *QuizData
	tool := NewQuizTool(func(quiz QuizData) {
		receivedQuiz = &quiz
	})

	ctx := context.Background()

	t.Run("successful execution", func(t *testing.T) {
		args := map[string]interface{}{
			"quiz_id":  "q1",
			"question": "What is opportunity cost?",
			"options": []interface{}{
				"A. The cost of production",
				"B. The value of the next best alternative",
				"C. The selling price",
			},
			"context": "Economics basics",
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result == "" {
			t.Error("Expected non-empty result")
		}

		if receivedQuiz == nil {
			t.Fatal("Quiz callback was not invoked")
		}

		if receivedQuiz.QuizID != "q1" {
			t.Errorf("Expected quiz_id 'q1', got '%s'", receivedQuiz.QuizID)
		}

		if receivedQuiz.Question != "What is opportunity cost?" {
			t.Errorf("Unexpected question: %s", receivedQuiz.Question)
		}

		if len(receivedQuiz.Options) != 3 {
			t.Errorf("Expected 3 options, got %d", len(receivedQuiz.Options))
		}

		if receivedQuiz.Context != "Economics basics" {
			t.Errorf("Unexpected context: %s", receivedQuiz.Context)
		}

		// 验证结果包含成功状态
		if result == "" || len(result) == 0 {
			t.Error("Result should not be empty")
		}
	})

	t.Run("missing quiz_id", func(t *testing.T) {
		args := map[string]interface{}{
			"question": "Test question",
			"options":  []interface{}{"A", "B"},
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error for missing quiz_id")
		}
	})

	t.Run("missing question", func(t *testing.T) {
		args := map[string]interface{}{
			"quiz_id": "q2",
			"options": []interface{}{"A", "B"},
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error for missing question")
		}
	})

	t.Run("missing options", func(t *testing.T) {
		args := map[string]interface{}{
			"quiz_id":  "q3",
			"question": "Test?",
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error for missing options")
		}
	})

	t.Run("invalid options type", func(t *testing.T) {
		args := map[string]interface{}{
			"quiz_id":  "q4",
			"question": "Test?",
			"options":  "not an array",
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error for invalid options type")
		}
	})
}

func TestQuizTool_PendingQuizzes(t *testing.T) {
	tool := NewQuizTool(nil)
	ctx := context.Background()

	// 初始应该为空
	pending := tool.GetPendingQuizzes()
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending quizzes, got %d", len(pending))
	}

	// 执行工具添加quiz
	args1 := map[string]interface{}{
		"quiz_id":  "q1",
		"question": "Question 1",
		"options":  []interface{}{"A", "B"},
	}
	_, _ = tool.Execute(ctx, args1)

	pending = tool.GetPendingQuizzes()
	if len(pending) != 1 {
		t.Fatalf("Expected 1 pending quiz, got %d", len(pending))
	}

	if pending[0].QuizID != "q1" {
		t.Errorf("Expected quiz_id 'q1', got '%s'", pending[0].QuizID)
	}

	// 添加第二个quiz
	args2 := map[string]interface{}{
		"quiz_id":  "q2",
		"question": "Question 2",
		"options":  []interface{}{"C", "D"},
	}
	_, _ = tool.Execute(ctx, args2)

	pending = tool.GetPendingQuizzes()
	if len(pending) != 2 {
		t.Fatalf("Expected 2 pending quizzes, got %d", len(pending))
	}

	// 清空队列
	tool.ClearPendingQuizzes()
	pending = tool.GetPendingQuizzes()
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending quizzes after clear, got %d", len(pending))
	}
}

func TestToolRegistry(t *testing.T) {
	registry := NewToolRegistry()

	// 注册quiz工具
	quizTool := NewQuizTool(nil)
	registry.Register(quizTool)

	// 检查能否获取
	tool, ok := registry.Get("show_quiz")
	if !ok {
		t.Fatal("Tool 'show_quiz' not found in registry")
	}

	if tool != quizTool {
		t.Error("Retrieved tool is not the same instance")
	}

	// 检查获取所有定义
	defs := registry.GetAllDefinitions()
	if len(defs) != 1 {
		t.Errorf("Expected 1 tool definition, got %d", len(defs))
	}

	if defs[0].Name != "show_quiz" {
		t.Errorf("Expected tool name 'show_quiz', got '%s'", defs[0].Name)
	}

	// 尝试获取不存在的工具
	_, ok = registry.Get("nonexistent")
	if ok {
		t.Error("Expected false for nonexistent tool")
	}
}

func TestToolRegistry_Execute(t *testing.T) {
	registry := NewToolRegistry()
	var executedQuiz *QuizData

	quizTool := NewQuizTool(func(quiz QuizData) {
		executedQuiz = &quiz
	})
	registry.Register(quizTool)

	ctx := context.Background()

	t.Run("execute registered tool", func(t *testing.T) {
		argsJSON := `{
			"quiz_id": "q1",
			"question": "Test question?",
			"options": ["A", "B", "C"]
		}`

		result, err := registry.Execute(ctx, "show_quiz", argsJSON)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if result == "" {
			t.Error("Expected non-empty result")
		}

		if executedQuiz == nil {
			t.Fatal("Quiz was not executed")
		}

		if executedQuiz.QuizID != "q1" {
			t.Errorf("Expected quiz_id 'q1', got '%s'", executedQuiz.QuizID)
		}
	})

	t.Run("execute nonexistent tool", func(t *testing.T) {
		_, err := registry.Execute(ctx, "nonexistent", "{}")
		if err == nil {
			t.Error("Expected error for nonexistent tool")
		}

		if _, ok := err.(*ToolNotFoundError); !ok {
			t.Errorf("Expected ToolNotFoundError, got %T", err)
		}
	})

	t.Run("execute with invalid JSON", func(t *testing.T) {
		_, err := registry.Execute(ctx, "show_quiz", "invalid json")
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}

		if _, ok := err.(*InvalidArgsError); !ok {
			t.Errorf("Expected InvalidArgsError, got %T", err)
		}
	})
}
