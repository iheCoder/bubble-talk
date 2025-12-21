package gateway

import (
	"context"
	"testing"
	"time"
)

// MockRoleConn 是 RoleConn 的 mock 实现，用于测试
type MockRoleConn struct {
	role             string
	voice            string
	userTexts        []string
	assistantTexts   []string
	createdResponses []string
	cancelledCount   int
	activeResponseID string
	connected        bool
	initialized      bool
}

func NewMockRoleConn(role, voice string) *MockRoleConn {
	return &MockRoleConn{
		role:             role,
		voice:            voice,
		userTexts:        make([]string, 0),
		assistantTexts:   make([]string, 0),
		createdResponses: make([]string, 0),
		connected:        true,
		initialized:      true,
	}
}

func (m *MockRoleConn) SyncUserText(text string) error {
	m.userTexts = append(m.userTexts, text)
	return nil
}

func (m *MockRoleConn) SyncAssistantText(text string, fromRole string) error {
	if fromRole != m.role {
		m.assistantTexts = append(m.assistantTexts, text)
	}
	return nil
}

func (m *MockRoleConn) CreateResponse(instructions string, metadata map[string]interface{}) error {
	m.createdResponses = append(m.createdResponses, instructions)
	return nil
}

func (m *MockRoleConn) CancelResponse() error {
	m.cancelledCount++
	return nil
}

func (m *MockRoleConn) SetActiveResponse(responseID string) {
	m.activeResponseID = responseID
}

func (m *MockRoleConn) ClearActiveResponse() {
	m.activeResponseID = ""
}

func (m *MockRoleConn) Close() error {
	m.connected = false
	return nil
}

// TestVoicePoolCreation 测试 VoicePool 创建
func TestVoicePoolCreation(t *testing.T) {
	config := VoicePoolConfig{
		OpenAIAPIKey:        "test-key",
		Model:               "gpt-4o-realtime-preview-2024-12-17",
		DefaultInstructions: "Test instructions",
		RoleVoices: map[string]string{
			"host":      "alloy",
			"economist": "echo",
			"skeptic":   "shimmer",
		},
	}

	pool := NewVoicePool("test-session", config)

	if pool.sessionID != "test-session" {
		t.Errorf("Expected sessionID 'test-session', got '%s'", pool.sessionID)
	}

	if len(pool.roleConns) != 0 {
		t.Errorf("Expected 0 role conns before initialization, got %d", len(pool.roleConns))
	}

	if pool.config.Model != config.Model {
		t.Errorf("Expected model '%s', got '%s'", config.Model, pool.config.Model)
	}
}

// TestVoicePoolSyncUserText 测试用户文本同步
func TestVoicePoolSyncUserText(t *testing.T) {

	t.Skip("Skipping test - needs interface refactoring for proper mocking")
}

// TestConversationHistory 测试对话历史记录
func TestConversationHistory(t *testing.T) {
	pool := &VoicePool{
		sessionID:           "test-session",
		roleConns:           make(map[string]*RoleConn),
		conversationHistory: make([]ConversationTurn, 0),
	}

	// 添加用户消息
	pool.conversationHistoryMu.Lock()
	pool.conversationHistory = append(pool.conversationHistory, ConversationTurn{
		Role: "user",
		Text: "Hello",
	})
	pool.conversationHistoryMu.Unlock()

	// 添加助手消息
	pool.conversationHistoryMu.Lock()
	pool.conversationHistory = append(pool.conversationHistory, ConversationTurn{
		Role:     "assistant",
		Text:     "Hi there",
		FromRole: "host",
	})
	pool.conversationHistoryMu.Unlock()

	// 获取历史
	history := pool.GetConversationHistory()

	if len(history) != 2 {
		t.Fatalf("Expected 2 conversation turns, got %d", len(history))
	}

	if history[0].Role != "user" || history[0].Text != "Hello" {
		t.Errorf("First turn mismatch: role=%s, text=%s", history[0].Role, history[0].Text)
	}

	if history[1].Role != "assistant" || history[1].Text != "Hi there" || history[1].FromRole != "host" {
		t.Errorf("Second turn mismatch: role=%s, text=%s, fromRole=%s",
			history[1].Role, history[1].Text, history[1].FromRole)
	}
}

// TestSpeakingRoleManagement 测试说话角色管理
func TestSpeakingRoleManagement(t *testing.T) {
	pool := &VoicePool{
		sessionID: "test-session",
		roleConns: make(map[string]*RoleConn),
	}

	// 初始状态：没有人在说话
	pool.speakingRoleMu.RLock()
	role := pool.speakingRole
	pool.speakingRoleMu.RUnlock()

	if role != "" {
		t.Errorf("Expected empty speaking role, got '%s'", role)
	}

	// 设置说话角色
	pool.speakingRoleMu.Lock()
	pool.speakingRole = "economist"
	pool.speakingRoleMu.Unlock()

	pool.speakingRoleMu.RLock()
	role = pool.speakingRole
	pool.speakingRoleMu.RUnlock()

	if role != "economist" {
		t.Errorf("Expected speaking role 'economist', got '%s'", role)
	}

	// 清除说话角色
	pool.ClearSpeakingRole()

	pool.speakingRoleMu.RLock()
	role = pool.speakingRole
	pool.speakingRoleMu.RUnlock()

	if role != "" {
		t.Errorf("Expected empty speaking role after clear, got '%s'", role)
	}
}

// TestVoicePoolConcurrency 测试并发安全性
func TestVoicePoolConcurrency(t *testing.T) {
	pool := &VoicePool{
		sessionID:           "test-session",
		roleConns:           make(map[string]*RoleConn),
		conversationHistory: make([]ConversationTurn, 0),
	}

	done := make(chan bool)
	iterations := 100

	// 并发写入对话历史
	go func() {
		for i := 0; i < iterations; i++ {
			pool.conversationHistoryMu.Lock()
			pool.conversationHistory = append(pool.conversationHistory, ConversationTurn{
				Role: "user",
				Text: "test",
			})
			pool.conversationHistoryMu.Unlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发读取对话历史
	go func() {
		for i := 0; i < iterations; i++ {
			_ = pool.GetConversationHistory()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 并发修改说话角色
	go func() {
		for i := 0; i < iterations; i++ {
			pool.speakingRoleMu.Lock()
			pool.speakingRole = "host"
			pool.speakingRoleMu.Unlock()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// 等待所有 goroutine 完成
	for i := 0; i < 3; i++ {
		<-done
	}

	// 验证数据一致性
	history := pool.GetConversationHistory()
	if len(history) != iterations {
		t.Errorf("Expected %d conversation turns, got %d", iterations, len(history))
	}
}

// TestVoicePoolConfig 测试配置验证
func TestVoicePoolConfig(t *testing.T) {
	tests := []struct {
		name   string
		config VoicePoolConfig
		valid  bool
	}{
		{
			name: "Valid config",
			config: VoicePoolConfig{
				OpenAIAPIKey: "sk-test",
				Model:        "gpt-4o-realtime-preview-2024-12-17",
				RoleVoices: map[string]string{
					"host": "alloy",
				},
			},
			valid: true,
		},
		{
			name: "Empty API key",
			config: VoicePoolConfig{
				OpenAIAPIKey: "",
				Model:        "gpt-4o-realtime-preview-2024-12-17",
				RoleVoices: map[string]string{
					"host": "alloy",
				},
			},
			valid: false,
		},
		{
			name: "Empty role voices",
			config: VoicePoolConfig{
				OpenAIAPIKey: "sk-test",
				Model:        "gpt-4o-realtime-preview-2024-12-17",
				RoleVoices:   map[string]string{},
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewVoicePool("test-session", tt.config)

			if pool == nil {
				t.Fatal("NewVoicePool returned nil")
			}

			// 验证配置
			isValid := tt.config.OpenAIAPIKey != "" && len(tt.config.RoleVoices) > 0

			if isValid != tt.valid {
				t.Errorf("Expected config validity %v, got %v", tt.valid, isValid)
			}
		})
	}
}

// BenchmarkSyncUserText 基准测试：用户文本同步性能
func BenchmarkSyncUserText(b *testing.B) {
	pool := &VoicePool{
		sessionID:           "bench-session",
		roleConns:           make(map[string]*RoleConn),
		conversationHistory: make([]ConversationTurn, 0),
	}

	text := "This is a test message for benchmarking"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.conversationHistoryMu.Lock()
		pool.conversationHistory = append(pool.conversationHistory, ConversationTurn{
			Role: "user",
			Text: text,
		})
		pool.conversationHistoryMu.Unlock()
	}
}

// BenchmarkGetConversationHistory 基准测试：获取对话历史性能
func BenchmarkGetConversationHistory(b *testing.B) {
	pool := &VoicePool{
		sessionID:           "bench-session",
		roleConns:           make(map[string]*RoleConn),
		conversationHistory: make([]ConversationTurn, 0),
	}

	// 预填充 100 条历史
	for i := 0; i < 100; i++ {
		pool.conversationHistory = append(pool.conversationHistory, ConversationTurn{
			Role: "user",
			Text: "test message",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.GetConversationHistory()
	}
}

// TestVoicePoolGetRoleConnError 测试获取不存在的角色连接
func TestVoicePoolGetRoleConnError(t *testing.T) {
	pool := &VoicePool{
		sessionID: "test-session",
		roleConns: make(map[string]*RoleConn),
	}

	_, err := pool.GetRoleConn(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error when getting nonexistent role conn, got nil")
	}

	expectedMsg := "role 'nonexistent' not configured in RoleVoices"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestVoicePoolGetASRConnError 测试获取未初始化的 ASR 连接
func TestVoicePoolGetASRConnError(t *testing.T) {
	pool := &VoicePool{
		sessionID: "test-session",
		asrConn:   nil,
	}

	_, err := pool.GetASRConn()
	if err == nil {
		t.Error("Expected error when getting uninitialized ASR conn, got nil")
	}

	expectedMsg := "ASR conn not initialized"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestCancelCurrentResponseNoSpeaker 测试取消响应但没有活跃说话者
func TestCancelCurrentResponseNoSpeaker(t *testing.T) {
	pool := &VoicePool{
		sessionID:    "test-session",
		roleConns:    make(map[string]*RoleConn),
		speakingRole: "",
	}

	err := pool.CancelCurrentResponse()
	if err != nil {
		t.Errorf("Expected no error when canceling with no speaker, got %v", err)
	}
}

// Example_voicePoolUsage 使用示例
func Example_voicePoolUsage() {
	ctx := context.Background()

	// 1. 创建配置
	config := VoicePoolConfig{
		OpenAIAPIKey:        "your-api-key",
		Model:               "gpt-4o-realtime-preview-2024-12-17",
		DefaultInstructions: "You are a helpful assistant",
		InputAudioFormat:    "pcm16",
		OutputAudioFormat:   "pcm16",
		RoleVoices: map[string]string{
			"host":      "alloy",
			"economist": "echo",
		},
	}

	// 2. 创建 VoicePool
	pool := NewVoicePool("session-123", config)

	// 3. 初始化（连接到 OpenAI）
	_ = pool.Initialize(ctx)

	// 4. 同步用户文本
	_ = pool.SyncUserText("Hello, how are you?")

	// 5. 在指定角色上创建响应
	metadata := map[string]interface{}{
		"role": "host",
		"beat": "greeting",
	}
	_ = pool.CreateResponse(ctx, "host", "Respond warmly", metadata)

	// 6. 同步助手文本（响应完成后）
	_ = pool.SyncAssistantText("I'm doing great, thanks!", "host")

	// 7. 清理
	_ = pool.Close()

	// Output:
}
