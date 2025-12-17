package writeback

import (
	"context"
	"errors"
	"sync"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

var (
	// ErrMemoryQueueClosed is returned when trying to enqueue to a closed memory queue.
	ErrMemoryQueueClosed = errors.New("memory queue is closed")
)

// MemoryQueue implements WriteBackQueue using an in-memory channel-based queue.
// This is useful for testing or when persistence is not required.
type MemoryQueue struct {
	queue  chan *core.WriteOperation
	mu     sync.RWMutex
	closed bool
	size   int
}

// NewMemoryQueue creates a new in-memory write-back queue.
// bufferSize is the maximum number of operations that can be buffered.
func NewMemoryQueue(bufferSize int) *MemoryQueue {
	if bufferSize <= 0 {
		bufferSize = 10000 // Default buffer size
	}

	return &MemoryQueue{
		queue: make(chan *core.WriteOperation, bufferSize),
		size:  bufferSize,
	}
}

// Enqueue adds a write operation to the queue.
func (q *MemoryQueue) Enqueue(ctx context.Context, operation *core.WriteOperation) error {
	q.mu.RLock()
	if q.closed {
		q.mu.RUnlock()
		return ErrMemoryQueueClosed
	}
	q.mu.RUnlock()

	select {
	case q.queue <- operation:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Queue is full
		return errors.New("memory queue is full")
	}
}

// Dequeue retrieves a batch of write operations from the queue.
// Returns operations in the order they were enqueued (FIFO).
func (q *MemoryQueue) Dequeue(ctx context.Context, batchSize int) ([]*core.WriteOperation, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	operations := make([]*core.WriteOperation, 0, batchSize)

	for i := 0; i < batchSize; i++ {
		select {
		case operation := <-q.queue:
			if operation == nil {
				// Channel closed
				return operations, nil
			}
			operations = append(operations, operation)
		case <-ctx.Done():
			return operations, ctx.Err()
		default:
			// No more operations available
			return operations, nil
		}
	}

	return operations, nil
}

// Size returns the current number of operations in the queue.
func (q *MemoryQueue) Size() int {
	return len(q.queue)
}

// Close closes the queue and prevents further enqueuing.
func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	close(q.queue)
	return nil
}
