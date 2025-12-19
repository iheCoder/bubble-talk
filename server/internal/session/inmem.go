package session

import (
	"context"
	"errors"
	"sync"

	"bubble-talk/server/internal/model"
)

var ErrNotFound = errors.New("session not found")

// InMemoryStore 是一个基于内存的 Session 存储实现。
type InMemoryStore struct {
	mu   sync.RWMutex
	data map[string]*model.SessionState
}

func NewInMemoryStore() *InMemoryStore {
	// 第一阶段用内存 store：实现简单、调试方便。
	// 注意：重启即丢数据；多人/多实例部署需要替换为 Redis/DB。
	return &InMemoryStore{data: make(map[string]*model.SessionState)}
}

// Get 根据 SessionID 获取 SessionState。
func (s *InMemoryStore) Get(_ context.Context, id string) (*model.SessionState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.data[id]
	if !ok {
		return nil, ErrNotFound
	}

	return state, nil
}

// Save 保存或更新 SessionState。
func (s *InMemoryStore) Save(_ context.Context, state *model.SessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[state.SessionID] = state
	return nil
}
