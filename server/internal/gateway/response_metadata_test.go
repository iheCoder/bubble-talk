package gateway

import (
	"testing"
	"time"
)

func TestResponseMetadataRegistry_RegisterAndGet(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	metadata := map[string]interface{}{
		"role":     "host",
		"beat":     "opening",
		"sequence": 1,
		"total":    3,
	}

	// 注册元数据
	registry.Register("resp_123", "host", metadata)

	// 获取元数据
	rm, ok := registry.Get("resp_123")
	if !ok {
		t.Fatal("Failed to get registered metadata")
	}

	if rm.ResponseID != "resp_123" {
		t.Errorf("Expected responseID=resp_123, got %s", rm.ResponseID)
	}
	if rm.Role != "host" {
		t.Errorf("Expected role=host, got %s", rm.Role)
	}
	if rm.Beat != "opening" {
		t.Errorf("Expected beat=opening, got %s", rm.Beat)
	}
	if rm.Sequence != 1 {
		t.Errorf("Expected sequence=1, got %d", rm.Sequence)
	}
}

func TestResponseMetadataRegistry_GetByRole(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	metadata1 := map[string]interface{}{
		"role": "host",
		"beat": "opening",
	}
	metadata2 := map[string]interface{}{
		"role": "economist",
		"beat": "deep_dive",
	}

	registry.Register("resp_1", "host", metadata1)
	registry.Register("resp_2", "economist", metadata2)

	// 通过角色获取最新元数据
	rm, ok := registry.GetByRole("host")
	if !ok {
		t.Fatal("Failed to get metadata by role")
	}
	if rm.ResponseID != "resp_1" {
		t.Errorf("Expected responseID=resp_1, got %s", rm.ResponseID)
	}

	// 同一角色注册新响应，应该更新索引
	metadata3 := map[string]interface{}{
		"role": "host",
		"beat": "wrap",
	}
	registry.Register("resp_3", "host", metadata3)

	rm, ok = registry.GetByRole("host")
	if !ok {
		t.Fatal("Failed to get updated metadata by role")
	}
	if rm.ResponseID != "resp_3" {
		t.Errorf("Expected updated responseID=resp_3, got %s", rm.ResponseID)
	}
	if rm.Beat != "wrap" {
		t.Errorf("Expected updated beat=wrap, got %s", rm.Beat)
	}
}

func TestResponseMetadataRegistry_Unregister(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	metadata := map[string]interface{}{
		"role": "host",
	}

	registry.Register("resp_1", "host", metadata)

	// 验证已注册
	if _, ok := registry.Get("resp_1"); !ok {
		t.Fatal("Metadata should be registered")
	}

	// 注销
	registry.Unregister("resp_1")

	// 验证已注销
	if _, ok := registry.Get("resp_1"); ok {
		t.Error("Metadata should be unregistered")
	}

	// 角色索引也应该被清除
	if _, ok := registry.GetByRole("host"); ok {
		t.Error("Role index should be cleared")
	}
}

func TestResponseMetadataRegistry_UnregisterNonLatest(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	metadata1 := map[string]interface{}{"role": "host"}
	metadata2 := map[string]interface{}{"role": "host"}

	registry.Register("resp_1", "host", metadata1)
	registry.Register("resp_2", "host", metadata2)

	// 注销旧的响应
	registry.Unregister("resp_1")

	// 角色索引应该仍然指向最新的
	rm, ok := registry.GetByRole("host")
	if !ok {
		t.Fatal("Role index should still exist")
	}
	if rm.ResponseID != "resp_2" {
		t.Errorf("Expected responseID=resp_2, got %s", rm.ResponseID)
	}

	// 注销最新的响应
	registry.Unregister("resp_2")

	// 现在角色索引应该被清除
	if _, ok := registry.GetByRole("host"); ok {
		t.Error("Role index should be cleared after unregistering latest")
	}
}

func TestResponseMetadataRegistry_Clear(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	registry.Register("resp_1", "host", map[string]interface{}{"role": "host"})
	registry.Register("resp_2", "economist", map[string]interface{}{"role": "economist"})

	if registry.Count() != 2 {
		t.Errorf("Expected count=2, got %d", registry.Count())
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Errorf("Expected count=0 after clear, got %d", registry.Count())
	}

	if _, ok := registry.Get("resp_1"); ok {
		t.Error("Metadata should be cleared")
	}
	if _, ok := registry.GetByRole("host"); ok {
		t.Error("Role index should be cleared")
	}
}

func TestResponseMetadataRegistry_GetActiveRoles(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	registry.Register("resp_1", "host", map[string]interface{}{"role": "host"})
	registry.Register("resp_2", "economist", map[string]interface{}{"role": "economist"})

	roles := registry.GetActiveRoles()
	if len(roles) != 2 {
		t.Errorf("Expected 2 active roles, got %d", len(roles))
	}

	// 验证包含两个角色（顺序不确定）
	roleMap := make(map[string]bool)
	for _, role := range roles {
		roleMap[role] = true
	}
	if !roleMap["host"] || !roleMap["economist"] {
		t.Error("Active roles should contain host and economist")
	}
}

func TestResponseMetadataRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	done := make(chan bool)

	// 并发注册
	for i := 0; i < 10; i++ {
		go func(id int) {
			metadata := map[string]interface{}{
				"role": "host",
				"id":   id,
			}
			registry.Register(string(rune('A'+id)), "host", metadata)
			done <- true
		}(i)
	}

	// 并发读取
	for i := 0; i < 10; i++ {
		go func() {
			_ = registry.GetActiveRoles()
			_, _ = registry.GetByRole("host")
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 20; i++ {
		<-done
	}

	// 验证最终状态一致
	if registry.Count() != 10 {
		t.Errorf("Expected count=10, got %d", registry.Count())
	}
}

func TestResponseMetadataRegistry_CreatedAtTracking(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	before := time.Now()
	registry.Register("resp_1", "host", map[string]interface{}{"role": "host"})
	after := time.Now()

	rm, ok := registry.Get("resp_1")
	if !ok {
		t.Fatal("Failed to get metadata")
	}

	if rm.CreatedAt.Before(before) || rm.CreatedAt.After(after) {
		t.Error("CreatedAt timestamp is incorrect")
	}
}

func TestResponseMetadataRegistry_EmptyMetadata(t *testing.T) {
	registry := NewResponseMetadataRegistry(nil)

	// 注册��元数据应该也能工作
	registry.Register("resp_1", "host", nil)

	rm, ok := registry.Get("resp_1")
	if !ok {
		t.Fatal("Failed to get metadata with nil input")
	}

	if rm.Metadata != nil {
		t.Error("Metadata should be nil")
	}
	if rm.Role != "host" {
		t.Errorf("Role should still be set: got %s", rm.Role)
	}
}
