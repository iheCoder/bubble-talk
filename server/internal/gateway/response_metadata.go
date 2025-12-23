package gateway

import (
	"sync"
	"time"
)

// ResponseMetadata 存储单个响应的元数据
type ResponseMetadata struct {
	ResponseID string
	Role       string
	Beat       string
	Sequence   int
	Total      int
	CreatedAt  time.Time
	Metadata   map[string]interface{} // 完整的元数据
}

// ResponseMetadataRegistry 管理所有响应的元数据
// 解决问题：元数据与音频流可靠关联，避免并发时角色错位
type ResponseMetadataRegistry struct {
	mu       sync.RWMutex
	registry map[string]*ResponseMetadata // key: responseID

	// 按角色索引（快速查找最新的响应）
	roleIndex map[string]string // role -> latest responseID

	logger Logger
}

// Logger 日志接口
type Logger interface {
	Printf(format string, v ...interface{})
}

// NewResponseMetadataRegistry 创建元数据注册表
func NewResponseMetadataRegistry(logger Logger) *ResponseMetadataRegistry {
	return &ResponseMetadataRegistry{
		registry:  make(map[string]*ResponseMetadata),
		roleIndex: make(map[string]string),
		logger:    logger,
	}
}

// Register 注册响应元数据
func (r *ResponseMetadataRegistry) Register(responseID, role string, metadata map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rm := &ResponseMetadata{
		ResponseID: responseID,
		Role:       role,
		CreatedAt:  time.Now(),
		Metadata:   metadata,
	}

	// 提取常用字段
	if beat, ok := metadata["beat"].(string); ok {
		rm.Beat = beat
	}
	if seq, ok := metadata["sequence"].(int); ok {
		rm.Sequence = seq
	}
	if total, ok := metadata["total"].(int); ok {
		rm.Total = total
	}

	r.registry[responseID] = rm
	r.roleIndex[role] = responseID

	if r.logger != nil {
		r.logger.Printf("[ResponseMetadataRegistry] Registered: responseID=%s role=%s beat=%s seq=%d/%d",
			responseID, role, rm.Beat, rm.Sequence, rm.Total)
	}
}

// Get 获取响应元数据
func (r *ResponseMetadataRegistry) Get(responseID string) (*ResponseMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rm, ok := r.registry[responseID]
	return rm, ok
}

// GetByRole 获取角色的最新响应元数据
func (r *ResponseMetadataRegistry) GetByRole(role string) (*ResponseMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	responseID, ok := r.roleIndex[role]
	if !ok {
		return nil, false
	}

	rm, ok := r.registry[responseID]
	return rm, ok
}

// Unregister 注销响应元数据
func (r *ResponseMetadataRegistry) Unregister(responseID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rm, ok := r.registry[responseID]
	if !ok {
		return
	}

	// 从注册表移除
	delete(r.registry, responseID)

	// 如果是该角色的最新响应，从角色索引移除
	if r.roleIndex[rm.Role] == responseID {
		delete(r.roleIndex, rm.Role)
	}

	if r.logger != nil {
		r.logger.Printf("[ResponseMetadataRegistry] Unregistered: responseID=%s role=%s",
			responseID, rm.Role)
	}
}

// Clear 清空所有元数据（用于会话结束）
func (r *ResponseMetadataRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := len(r.registry)
	r.registry = make(map[string]*ResponseMetadata)
	r.roleIndex = make(map[string]string)

	if r.logger != nil {
		r.logger.Printf("[ResponseMetadataRegistry] Cleared %d metadata entries", count)
	}
}

// GetActiveRoles 获取当前有活跃响应的角色列表
func (r *ResponseMetadataRegistry) GetActiveRoles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	roles := make([]string, 0, len(r.roleIndex))
	for role := range r.roleIndex {
		roles = append(roles, role)
	}
	return roles
}

// Count 获取当前注册的响应数量
func (r *ResponseMetadataRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.registry)
}
