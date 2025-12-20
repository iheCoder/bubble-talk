package timeline

import (
	"context"
	"sync"

	"bubble-talk/server/internal/model"
)

// InMemoryStore 是一个基于内存的 Timeline 存储实现。
type InMemoryStore struct {
	mu       sync.RWMutex
	events   map[string][]model.Event
	seq      map[string]int64
	eventIDs map[string]map[string]int64
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		events:   make(map[string][]model.Event),
		seq:      make(map[string]int64),
		eventIDs: make(map[string]map[string]int64),
	}
}

// Append 追加事件到 timeline，并为该 session 分配单调递增 seq。
// 副作用：会修改内存状态；相同 EventID 会直接返回已分配的 seq（幂等）。
func (s *InMemoryStore) Append(_ context.Context, sessionID string, evt *model.Event) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if evt.EventID != "" {
		if seen, ok := s.eventIDs[sessionID]; ok {
			if seq, exists := seen[evt.EventID]; exists {
				return seq, nil
			}
		}
	}

	s.seq[sessionID]++
	seq := s.seq[sessionID]

	eventCopy := *evt
	eventCopy.Seq = seq
	eventCopy.SessionID = sessionID
	s.events[sessionID] = append(s.events[sessionID], eventCopy)

	if evt.EventID != "" {
		if s.eventIDs[sessionID] == nil {
			s.eventIDs[sessionID] = make(map[string]int64)
		}
		s.eventIDs[sessionID][evt.EventID] = seq
	}

	return seq, nil
}

// List 返回某个 session 的全部 timeline 事件（按 seq 顺序）。
// 兼容性：返回切片副本，避免调用方修改内部数据。
func (s *InMemoryStore) List(_ context.Context, sessionID string) ([]model.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.events[sessionID]
	out := make([]model.Event, len(events))
	copy(out, events)
	return out, nil
}
