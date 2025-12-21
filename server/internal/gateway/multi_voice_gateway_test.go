package gateway

import (
	"context"
	"testing"
	"time"
)

// TestMultiVoiceGatewayCreation 测试 MultiVoiceGateway 创建
func TestMultiVoiceGatewayCreation(t *testing.T) {
	config := GatewayConfig{
		OpenAIAPIKey: "test-key",
		Model:        "gpt-4o-realtime-preview-2024-12-17",
		RoleProfiles: map[string]RoleProfile{
			"host": {
				Voice:  "alloy",
				Avatar: "host.png",
			},
			"economist": {
				Voice:  "echo",
				Avatar: "economist.png",
			},
		},
		DefaultInstructions: "Test instructions",
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	if gw.sessionID != "test-session" {
		t.Errorf("Expected sessionID 'test-session', got '%s'", gw.sessionID)
	}

	if gw.config.Model != config.Model {
		t.Errorf("Expected model '%s', got '%s'", config.Model, gw.config.Model)
	}
}

// TestMultiVoiceGatewayDone 测试 Done channel
func TestMultiVoiceGatewayDone(t *testing.T) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	// Done channel 应该在初始状态下是开放的
	select {
	case <-gw.Done():
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}

	// 关闭网关
	err := gw.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}

	// Done channel 应该被关闭
	select {
	case <-gw.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Close()")
	}
}

// TestMultiVoiceGatewayEventHandler 测试事件处理器设置
func TestMultiVoiceGatewayEventHandler(t *testing.T) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	// 初始状态：无事件处理器
	if gw.eventHandler != nil {
		t.Error("Expected nil event handler initially")
	}

	// 设置事件处理器
	handlerCalled := false
	handler := func(ctx context.Context, msg *ClientMessage) error {
		handlerCalled = true
		return nil
	}

	gw.SetEventHandler(handler)

	if gw.eventHandler == nil {
		t.Error("Expected non-nil event handler after SetEventHandler")
	}

	// 验证处理器可以被调用
	_ = gw.eventHandler(context.Background(), &ClientMessage{})

	if !handlerCalled {
		t.Error("Expected handler to be called")
	}
}

// TestMultiVoiceGatewayMetadata 测试元数据管理
func TestMultiVoiceGatewayMetadata(t *testing.T) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	// 初始状态：无活跃元数据
	gw.activeMetadataLock.RLock()
	metadata := gw.activeMetadata
	gw.activeMetadataLock.RUnlock()

	if metadata != nil {
		t.Errorf("Expected nil active metadata, got %v", metadata)
	}

	// 设置元数据
	testMetadata := map[string]interface{}{
		"role": "host",
		"beat": "greeting",
	}

	gw.activeMetadataLock.Lock()
	gw.activeMetadata = testMetadata
	gw.activeMetadataLock.Unlock()

	// 验证元数据
	gw.activeMetadataLock.RLock()
	metadata = gw.activeMetadata
	gw.activeMetadataLock.RUnlock()

	if metadata["role"] != "host" {
		t.Errorf("Expected role 'host', got %v", metadata["role"])
	}

	if metadata["beat"] != "greeting" {
		t.Errorf("Expected beat 'greeting', got %v", metadata["beat"])
	}
}

// TestMultiVoiceGatewaySequenceNumber 测试序列号生成
func TestMultiVoiceGatewaySequenceNumber(t *testing.T) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	// 初始序列号应该为 0
	if gw.seqCounter != 0 {
		t.Errorf("Expected initial seqCounter 0, got %d", gw.seqCounter)
	}

	// 模拟发送消息（增加序列号）
	for i := 1; i <= 10; i++ {
		gw.seqLock.Lock()
		gw.seqCounter++
		seq := gw.seqCounter
		gw.seqLock.Unlock()

		if seq != int64(i) {
			t.Errorf("Expected sequence number %d, got %d", i, seq)
		}
	}
}

// TestMultiVoiceGatewayConcurrency 测试并发安全性
func TestMultiVoiceGatewayConcurrency(t *testing.T) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("test-session", nil, config)

	done := make(chan bool)
	iterations := 100

	// 并发修改元数据
	go func() {
		for i := 0; i < iterations; i++ {
			gw.activeMetadataLock.Lock()
			gw.activeMetadata = map[string]interface{}{"test": i}
			gw.activeMetadataLock.Unlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发读取元数据
	go func() {
		for i := 0; i < iterations; i++ {
			gw.activeMetadataLock.RLock()
			_ = gw.activeMetadata
			gw.activeMetadataLock.RUnlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发增加序列号
	go func() {
		for i := 0; i < iterations; i++ {
			gw.seqLock.Lock()
			gw.seqCounter++
			gw.seqLock.Unlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 等待所有 goroutine 完成
	for i := 0; i < 3; i++ {
		<-done
	}

	// 验证序列号
	gw.seqLock.Lock()
	seq := gw.seqCounter
	gw.seqLock.Unlock()

	if seq != int64(iterations) {
		t.Errorf("Expected final seqCounter %d, got %d", iterations, seq)
	}
}

// TestMultiVoiceGatewayRoleProfiles 测试角色配置
func TestMultiVoiceGatewayRoleProfiles(t *testing.T) {
	tests := []struct {
		name         string
		roleProfiles map[string]RoleProfile
		expectedLen  int
	}{
		{
			name: "Single role",
			roleProfiles: map[string]RoleProfile{
				"host": {Voice: "alloy"},
			},
			expectedLen: 1,
		},
		{
			name: "Multiple roles",
			roleProfiles: map[string]RoleProfile{
				"host":      {Voice: "alloy", Avatar: "host.png"},
				"economist": {Voice: "echo", Avatar: "economist.png"},
				"skeptic":   {Voice: "shimmer", Avatar: "skeptic.png"},
			},
			expectedLen: 3,
		},
		{
			name:         "Empty roles",
			roleProfiles: map[string]RoleProfile{},
			expectedLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := GatewayConfig{
				RoleProfiles: tt.roleProfiles,
			}

			gw := NewMultiVoiceGateway("test-session", nil, config)

			if len(gw.config.RoleProfiles) != tt.expectedLen {
				t.Errorf("Expected %d role profiles, got %d", tt.expectedLen, len(gw.config.RoleProfiles))
			}

			// 验证每个角色配置
			for role, profile := range tt.roleProfiles {
				gotProfile, ok := gw.config.RoleProfiles[role]
				if !ok {
					t.Errorf("Role '%s' not found in config", role)
					continue
				}

				if gotProfile.Voice != profile.Voice {
					t.Errorf("Role '%s': expected voice '%s', got '%s'", role, profile.Voice, gotProfile.Voice)
				}

				if gotProfile.Avatar != profile.Avatar {
					t.Errorf("Role '%s': expected avatar '%s', got '%s'", role, profile.Avatar, gotProfile.Avatar)
				}
			}
		})
	}
}

// BenchmarkMultiVoiceGatewaySeqCounter 基准测试：序列号生成性能
func BenchmarkMultiVoiceGatewaySeqCounter(b *testing.B) {
	config := GatewayConfig{
		RoleProfiles: map[string]RoleProfile{
			"host": {Voice: "alloy"},
		},
	}

	gw := NewMultiVoiceGateway("bench-session", nil, config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gw.seqLock.Lock()
		gw.seqCounter++
		gw.seqLock.Unlock()
	}
}

// Example_multiVoiceGatewayUsage MultiVoiceGateway 使用示例
func Example_multiVoiceGatewayUsage() {
	ctx := context.Background()

	// 1. 创建配置
	config := GatewayConfig{
		OpenAIAPIKey: "your-api-key",
		Model:        "gpt-4o-realtime-preview-2024-12-17",
		RoleProfiles: map[string]RoleProfile{
			"host": {
				Voice:  "alloy",
				Avatar: "host.png",
			},
			"economist": {
				Voice:  "echo",
				Avatar: "economist.png",
			},
		},
		DefaultInstructions:          "You are a helpful assistant",
		InputAudioFormat:             "pcm16",
		OutputAudioFormat:            "pcm16",
		InputAudioTranscriptionModel: "whisper-1",
	}

	// 2. 创建 MultiVoiceGateway
	// 注意：需要一个有效的 WebSocket 连接，这里用 nil 仅作示例
	gw := NewMultiVoiceGateway("session-123", nil, config)

	// 3. 设置事件处理器
	gw.SetEventHandler(func(ctx context.Context, msg *ClientMessage) error {
		// 处理来自客户端的事件
		return nil
	})

	// 4. 启动网关
	_ = gw.Start(ctx)

	// 5. 发送指令到指定角色
	metadata := map[string]interface{}{
		"role": "host",
		"beat": "greeting",
	}
	_ = gw.SendInstructions(ctx, "Welcome the user", metadata)

	// 6. 关闭网关
	_ = gw.Close()

	// Output:
}
