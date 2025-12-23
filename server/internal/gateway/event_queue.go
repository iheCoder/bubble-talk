package gateway

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// EventQueue 为单个会话提供串行事件处理（Actor Model）
// 解决问题：
// 1. 防止 SessionState 并发修改导致的数据竞态
// 2. 保证事件处理顺序，避免 asr_final 和 assistant_text 乱序
type EventQueue struct {
	sessionID    string
	eventHandler EventHandler
	eventChan    chan *queuedEvent
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	logger       *log.Logger

	// 统计信息
	mu              sync.Mutex
	totalEvents     int64
	processedEvents int64
	droppedEvents   int64
}

type queuedEvent struct {
	msg       *ClientMessage
	timestamp time.Time
	resultCh  chan error // 用于同步等待结果（可选）
}

const (
	// 队列容量：超过此值的事件将被丢弃（背压控制）
	defaultQueueCapacity = 100
	// 事件处理超时
	defaultEventTimeout = 10 * time.Second
)

// NewEventQueue 创建事件队列
func NewEventQueue(sessionID string, handler EventHandler, logger *log.Logger) *EventQueue {
	if logger == nil {
		logger = log.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	eq := &EventQueue{
		sessionID:    sessionID,
		eventHandler: handler,
		eventChan:    make(chan *queuedEvent, defaultQueueCapacity),
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
	}

	// 启动单线程事件处理器
	eq.wg.Add(1)
	go eq.processLoop()

	logger.Printf("[EventQueue] Created for session %s", sessionID)

	return eq
}

// Enqueue 将事件加入队列（异步，非阻塞）
func (eq *EventQueue) Enqueue(msg *ClientMessage) error {
	select {
	case <-eq.ctx.Done():
		return fmt.Errorf("event queue closed")
	default:
	}

	event := &queuedEvent{
		msg:       msg,
		timestamp: time.Now(),
	}

	select {
	case eq.eventChan <- event:
		eq.mu.Lock()
		eq.totalEvents++
		eq.mu.Unlock()
		eq.logger.Printf("[EventQueue] Enqueued event: type=%s queue_size=%d", msg.Type, len(eq.eventChan))
		return nil
	default:
		// 队列已满，丢弃事件（背压控制）
		eq.mu.Lock()
		eq.droppedEvents++
		eq.mu.Unlock()
		eq.logger.Printf("[EventQueue] ⚠️  Queue full, dropping event: type=%s", msg.Type)
		return fmt.Errorf("event queue full")
	}
}

// EnqueueSync 将事件加入队列并等待处理完成（同步）
func (eq *EventQueue) EnqueueSync(msg *ClientMessage, timeout time.Duration) error {
	select {
	case <-eq.ctx.Done():
		return fmt.Errorf("event queue closed")
	default:
	}

	if timeout == 0 {
		timeout = defaultEventTimeout
	}

	event := &queuedEvent{
		msg:       msg,
		timestamp: time.Now(),
		resultCh:  make(chan error, 1),
	}

	select {
	case eq.eventChan <- event:
		eq.mu.Lock()
		eq.totalEvents++
		eq.mu.Unlock()
	case <-time.After(timeout):
		return fmt.Errorf("timeout enqueuing event")
	case <-eq.ctx.Done():
		return fmt.Errorf("event queue closed")
	}

	// 等待处理结果
	select {
	case err := <-event.resultCh:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for event processing")
	case <-eq.ctx.Done():
		return fmt.Errorf("event queue closed")
	}
}

// processLoop 串行处理事件（单线程）
func (eq *EventQueue) processLoop() {
	defer eq.wg.Done()

	eq.logger.Printf("[EventQueue] Process loop started for session %s", eq.sessionID)

	for {
		select {
		case <-eq.ctx.Done():
			eq.logger.Printf("[EventQueue] Process loop stopped for session %s", eq.sessionID)
			return

		case event := <-eq.eventChan:
			eq.processEvent(event)
		}
	}
}

// processEvent 处理单个事件
func (eq *EventQueue) processEvent(event *queuedEvent) {
	startTime := time.Now()
	queueLatency := startTime.Sub(event.timestamp)

	eq.logger.Printf("[EventQueue] Processing event: type=%s queue_latency=%v",
		event.msg.Type, queueLatency)

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(eq.ctx, defaultEventTimeout)
	defer cancel()

	// 调用事件处理器
	err := eq.eventHandler(ctx, event.msg)

	processingTime := time.Since(startTime)

	if err != nil {
		eq.logger.Printf("[EventQueue] ❌ Event processing failed: type=%s error=%v processing_time=%v",
			event.msg.Type, err, processingTime)
	} else {
		eq.logger.Printf("[EventQueue] ✅ Event processed successfully: type=%s processing_time=%v",
			event.msg.Type, processingTime)
	}

	eq.mu.Lock()
	eq.processedEvents++
	eq.mu.Unlock()

	// 如果是同步调用，返回结果
	if event.resultCh != nil {
		select {
		case event.resultCh <- err:
		default:
		}
	}

	// 监控：如果处理时间过长，记录警告
	if processingTime > 5*time.Second {
		eq.logger.Printf("[EventQueue] ⚠️  Slow event processing: type=%s processing_time=%v",
			event.msg.Type, processingTime)
	}
}

// Close 关闭事件队列
func (eq *EventQueue) Close() error {
	eq.cancel()

	// 等待处理器退出
	eq.wg.Wait()

	// 记录统计信息
	eq.mu.Lock()
	total := eq.totalEvents
	processed := eq.processedEvents
	dropped := eq.droppedEvents
	eq.mu.Unlock()

	eq.logger.Printf("[EventQueue] Closed for session %s: total=%d processed=%d dropped=%d pending=%d",
		eq.sessionID, total, processed, dropped, len(eq.eventChan))

	close(eq.eventChan)

	return nil
}

// GetStats 获取队列统计信息
func (eq *EventQueue) GetStats() map[string]interface{} {
	eq.mu.Lock()
	defer eq.mu.Unlock()

	return map[string]interface{}{
		"session_id":       eq.sessionID,
		"total_events":     eq.totalEvents,
		"processed_events": eq.processedEvents,
		"dropped_events":   eq.droppedEvents,
		"pending_events":   len(eq.eventChan),
		"queue_capacity":   cap(eq.eventChan),
	}
}
