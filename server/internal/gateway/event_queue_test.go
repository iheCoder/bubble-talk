package gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventQueue_SerialProcessing(t *testing.T) {
	var processedEvents []string
	var mu sync.Mutex

	handler := func(ctx context.Context, msg *ClientMessage) error {
		mu.Lock()
		defer mu.Unlock()
		processedEvents = append(processedEvents, string(msg.Type))
		time.Sleep(10 * time.Millisecond) // 模拟处理时间
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 快速发送多个事件
	events := []string{"event1", "event2", "event3", "event4", "event5"}
	for _, eventType := range events {
		err := eq.Enqueue(&ClientMessage{
			Type:    EventType(eventType),
			EventID: eventType,
		})
		if err != nil {
			t.Fatalf("Failed to enqueue event: %v", err)
		}
	}

	// 等待所有事件处理完成
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// 验证事件按顺序处理
	if len(processedEvents) != len(events) {
		t.Errorf("Expected %d processed events, got %d", len(events), len(processedEvents))
	}

	for i, event := range events {
		if processedEvents[i] != event {
			t.Errorf("Event order mismatch at index %d: expected %s, got %s",
				i, event, processedEvents[i])
		}
	}
}

func TestEventQueue_ConcurrentEnqueue(t *testing.T) {
	var processedCount int64
	handler := func(ctx context.Context, msg *ClientMessage) error {
		atomic.AddInt64(&processedCount, 1)
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 并发发送事件
	numGoroutines := 10
	eventsPerGoroutine := 10
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				_ = eq.Enqueue(&ClientMessage{
					Type:    "test",
					EventID: "test",
				})
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	expectedCount := int64(numGoroutines * eventsPerGoroutine)
	actualCount := atomic.LoadInt64(&processedCount)

	if actualCount != expectedCount {
		t.Errorf("Expected %d processed events, got %d", expectedCount, actualCount)
	}
}

func TestEventQueue_BackPressure(t *testing.T) {
	// 创建一个慢处理器
	handler := func(ctx context.Context, msg *ClientMessage) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 快速发送超过队列容量的事件
	droppedCount := 0
	for i := 0; i < defaultQueueCapacity+50; i++ {
		err := eq.Enqueue(&ClientMessage{
			Type:    "test",
			EventID: "test",
		})
		if err != nil {
			droppedCount++
		}
	}

	// 应该有一些事件被丢弃
	if droppedCount == 0 {
		t.Error("Expected some events to be dropped due to backpressure")
	}

	stats := eq.GetStats()
	t.Logf("Stats: %+v", stats)
}

func TestEventQueue_ErrorHandling(t *testing.T) {
	testError := errors.New("test error")
	handler := func(ctx context.Context, msg *ClientMessage) error {
		if msg.Type == "error_event" {
			return testError
		}
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 发送正常事件和错误事件
	_ = eq.Enqueue(&ClientMessage{Type: "normal", EventID: "1"})
	_ = eq.Enqueue(&ClientMessage{Type: "error_event", EventID: "2"})
	_ = eq.Enqueue(&ClientMessage{Type: "normal", EventID: "3"})

	time.Sleep(100 * time.Millisecond)

	stats := eq.GetStats()
	// 即使有错误，所有事件都应该被处理
	if stats["processed_events"].(int64) != 3 {
		t.Errorf("Expected 3 processed events, got %v", stats["processed_events"])
	}
}

func TestEventQueue_SyncEnqueue(t *testing.T) {
	var processedEvents []string
	var mu sync.Mutex

	handler := func(ctx context.Context, msg *ClientMessage) error {
		mu.Lock()
		defer mu.Unlock()
		processedEvents = append(processedEvents, string(msg.Type))
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 同步发送事件
	err := eq.EnqueueSync(&ClientMessage{
		Type:    "sync_event",
		EventID: "sync1",
	}, 1*time.Second)

	if err != nil {
		t.Fatalf("Sync enqueue failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(processedEvents) != 1 || processedEvents[0] != "sync_event" {
		t.Errorf("Sync event not processed correctly")
	}
}

func TestEventQueue_Timeout(t *testing.T) {
	// 创建一个会超时的处理器
	handler := func(ctx context.Context, msg *ClientMessage) error {
		time.Sleep(20 * time.Second) // 超过默认超时时间
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	// 同步发送，应该超时
	err := eq.EnqueueSync(&ClientMessage{
		Type:    "slow_event",
		EventID: "slow1",
	}, 100*time.Millisecond)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestEventQueue_CloseWhileProcessing(t *testing.T) {
	var processedCount int64
	handler := func(ctx context.Context, msg *ClientMessage) error {
		atomic.AddInt64(&processedCount, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)

	// 发送一些事件
	for i := 0; i < 10; i++ {
		_ = eq.Enqueue(&ClientMessage{
			Type:    "test",
			EventID: "test",
		})
	}

	// 立即关闭
	time.Sleep(10 * time.Millisecond)
	err := eq.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证统计信息
	stats := eq.GetStats()
	t.Logf("Final stats: %+v", stats)
}

func BenchmarkEventQueue_Enqueue(b *testing.B) {
	handler := func(ctx context.Context, msg *ClientMessage) error {
		return nil
	}

	eq := NewEventQueue("test-session", handler, nil)
	defer eq.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eq.Enqueue(&ClientMessage{
			Type:    "test",
			EventID: "test",
		})
	}
}
