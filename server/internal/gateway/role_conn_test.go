package gateway

import (
	"context"
	"testing"
	"time"
)

// TestRoleConnCreation 测试 RoleConn 创建
func TestRoleConnCreation(t *testing.T) {
	config := RoleConnConfig{
		OpenAIAPIKey:      "test-key",
		Model:             "gpt-4o-realtime-preview-2024-12-17",
		Voice:             "alloy",
		Instructions:      "Test instructions",
		EnableAudioOutput: true,
	}

	conn := NewRoleConn("host", "alloy", config)

	if conn.role != "host" {
		t.Errorf("Expected role 'host', got '%s'", conn.role)
	}

	if conn.voice != "alloy" {
		t.Errorf("Expected voice 'alloy', got '%s'", conn.voice)
	}

	if conn.config.Voice != "alloy" {
		t.Errorf("Expected config voice 'alloy', got '%s'", conn.config.Voice)
	}
}

// TestRoleConnActiveResponse 测试活跃响应管理
func TestRoleConnActiveResponse(t *testing.T) {
	config := RoleConnConfig{
		Voice: "alloy",
	}

	conn := NewRoleConn("host", "alloy", config)

	// 初始状态：无活跃响应
	conn.activeResponseIDLock.RLock()
	responseID := conn.activeResponseID
	conn.activeResponseIDLock.RUnlock()

	if responseID != "" {
		t.Errorf("Expected empty active response ID, got '%s'", responseID)
	}

	// 设置活跃响应
	conn.SetActiveResponse("resp-123")

	conn.activeResponseIDLock.RLock()
	responseID = conn.activeResponseID
	conn.activeResponseIDLock.RUnlock()

	if responseID != "resp-123" {
		t.Errorf("Expected active response ID 'resp-123', got '%s'", responseID)
	}

	// 清除活跃响应
	conn.ClearActiveResponse()

	conn.activeResponseIDLock.RLock()
	responseID = conn.activeResponseID
	conn.activeResponseIDLock.RUnlock()

	if responseID != "" {
		t.Errorf("Expected empty active response ID after clear, got '%s'", responseID)
	}
}

// TestRoleConnDone 测试 Done channel
func TestRoleConnDone(t *testing.T) {
	config := RoleConnConfig{
		Voice: "alloy",
	}

	conn := NewRoleConn("host", "alloy", config)

	// Done channel 应该在初始状态下是开放的
	select {
	case <-conn.Done():
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}

	// 关闭连接
	err := conn.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}

	// Done channel 应该被关闭
	select {
	case <-conn.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Close()")
	}

	// 再次关闭应该是安全的（幂等性）
	err = conn.Close()
	if err != nil {
		t.Errorf("Expected no error on second close, got %v", err)
	}
}

// TestRoleConnConcurrency 测试 RoleConn 并发安全性
func TestRoleConnConcurrency(t *testing.T) {
	config := RoleConnConfig{
		Voice: "alloy",
	}

	conn := NewRoleConn("host", "alloy", config)

	done := make(chan bool)
	iterations := 100

	// 并发设置活跃响应
	go func() {
		for i := 0; i < iterations; i++ {
			conn.SetActiveResponse("resp-1")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发清除活跃响应
	go func() {
		for i := 0; i < iterations; i++ {
			conn.ClearActiveResponse()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发读取活跃响应
	go func() {
		for i := 0; i < iterations; i++ {
			conn.activeResponseIDLock.RLock()
			_ = conn.activeResponseID
			conn.activeResponseIDLock.RUnlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 等待所有 goroutine 完成
	for i := 0; i < 3; i++ {
		<-done
	}

	// 测试通过意味着没有 panic（数据竞争）
}

// TestRoleConnConfig 测试不同配置场景
func TestRoleConnConfig(t *testing.T) {
	tests := []struct {
		name   string
		config RoleConnConfig
		role   string
		voice  string
	}{
		{
			name: "Full config",
			config: RoleConnConfig{
				OpenAIAPIKey:                 "sk-test",
				Model:                        "gpt-4o-realtime-preview-2024-12-17",
				Voice:                        "alloy",
				Instructions:                 "Test",
				InputAudioFormat:             "pcm16",
				OutputAudioFormat:            "pcm16",
				InputAudioTranscriptionModel: "whisper-1",
				EnableAudioOutput:            true,
			},
			role:  "host",
			voice: "alloy",
		},
		{
			name: "Minimal config",
			config: RoleConnConfig{
				Voice: "echo",
			},
			role:  "economist",
			voice: "echo",
		},
		{
			name: "ASR config",
			config: RoleConnConfig{
				Voice:             "alloy",
				EnableAudioOutput: false,
			},
			role:  "asr",
			voice: "alloy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewRoleConn(tt.role, tt.voice, tt.config)

			if conn.role != tt.role {
				t.Errorf("Expected role '%s', got '%s'", tt.role, conn.role)
			}

			if conn.voice != tt.voice {
				t.Errorf("Expected voice '%s', got '%s'", tt.voice, conn.voice)
			}

			if conn.config.EnableAudioOutput != tt.config.EnableAudioOutput {
				t.Errorf("Expected EnableAudioOutput %v, got %v",
					tt.config.EnableAudioOutput, conn.config.EnableAudioOutput)
			}
		})
	}
}

// BenchmarkRoleConnSetActiveResponse 基准测试：设置活跃响应性能
func BenchmarkRoleConnSetActiveResponse(b *testing.B) {
	config := RoleConnConfig{
		Voice: "alloy",
	}
	conn := NewRoleConn("host", "alloy", config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.SetActiveResponse("resp-123")
	}
}

// BenchmarkRoleConnClearActiveResponse 基准测试：清除活跃响应性能
func BenchmarkRoleConnClearActiveResponse(b *testing.B) {
	config := RoleConnConfig{
		Voice: "alloy",
	}
	conn := NewRoleConn("host", "alloy", config)
	conn.SetActiveResponse("resp-123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.ClearActiveResponse()
		conn.SetActiveResponse("resp-123") // 重新设置以便下次清除
	}
}

// Example_roleConnUsage RoleConn 使用示例
func Example_roleConnUsage() {
	ctx := context.Background()

	// 1. 创建配置
	config := RoleConnConfig{
		OpenAIAPIKey:      "your-api-key",
		Model:             "gpt-4o-realtime-preview-2024-12-17",
		Voice:             "alloy",
		Instructions:      "You are a helpful host",
		InputAudioFormat:  "pcm16",
		OutputAudioFormat: "pcm16",
		EnableAudioOutput: true,
	}

	// 2. 创建 RoleConn
	conn := NewRoleConn("host", "alloy", config)

	// 3. 连接到 OpenAI
	_ = conn.Connect(ctx)

	// 4. 初始化会话（固定 voice）
	_ = conn.Initialize(ctx)

	// 5. 同步用户文本
	_ = conn.SyncUserText("Hello")

	// 6. 创建响应
	metadata := map[string]interface{}{
		"role": "host",
	}
	_ = conn.CreateResponse("Respond warmly", metadata)

	// 7. 同步其他角色的文本
	_ = conn.SyncAssistantText("I agree", "economist")

	// 8. 关闭连接
	_ = conn.Close()

	// Output:
}

// TestRoleConnContextCancellation 测试上下文取消
func TestRoleConnContextCancellation(t *testing.T) {
	config := RoleConnConfig{
		Voice: "alloy",
	}

	conn := NewRoleConn("host", "alloy", config)

	// 检查上下文初始状态
	select {
	case <-conn.ctx.Done():
		t.Error("Context should not be cancelled initially")
	default:
		// Expected
	}

	// 关闭连接应该取消上下文
	err := conn.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}

	// 等待上下文被取消
	select {
	case <-conn.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled after Close()")
	}
}

// TestRoleConnMultipleVoices 测试不同音色的 RoleConn
func TestRoleConnMultipleVoices(t *testing.T) {
	voices := []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}

	for _, voice := range voices {
		t.Run(voice, func(t *testing.T) {
			config := RoleConnConfig{
				Voice: voice,
			}

			conn := NewRoleConn("test", voice, config)

			if conn.voice != voice {
				t.Errorf("Expected voice '%s', got '%s'", voice, conn.voice)
			}

			if conn.config.Voice != voice {
				t.Errorf("Expected config voice '%s', got '%s'", voice, conn.config.Voice)
			}
		})
	}
}
