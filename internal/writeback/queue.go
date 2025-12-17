package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

var (
	// ErrQueueClosed is returned when trying to enqueue to a closed queue.
	ErrQueueClosed = errors.New("write-back queue is closed")

	// ErrInvalidOperation is returned when an invalid operation is provided.
	ErrInvalidOperation = errors.New("invalid write operation")

	// ErrRedisOperationsNotSupported is returned when the KVStore doesn't support Redis list operations.
	ErrRedisOperationsNotSupported = errors.New("KVStore does not support Redis list operations")
)

// RedisQueue implements WriteBackQueue using Redis Lists.
// Operations are stored in Redis Lists for persistence and distributed access.
// The KVStore must implement RedisQueueOperations for this queue to work properly.
type RedisQueue struct {
	kvStore core.KVStore
	ops     RedisQueueOperations
	prefix  string
	closed  bool
}

// NewRedisQueue creates a new Redis-based write-back queue.
// prefix is used to namespace queue keys in Redis (e.g., "wbq:").
// The kvStore should implement RedisQueueOperations for optimal performance.
func NewRedisQueue(kvStore core.KVStore, prefix string) *RedisQueue {
	if prefix == "" {
		prefix = "wbq"
	}

	var ops RedisQueueOperations
	if opsImpl, ok := kvStore.(RedisQueueOperations); ok {
		ops = opsImpl
	}

	return &RedisQueue{
		kvStore: kvStore,
		ops:     ops,
		prefix:  prefix,
	}
}

// getQueueKey returns the Redis key for the queue of a specific table.
func (q *RedisQueue) getQueueKey(table string) string {
	return fmt.Sprintf("%s:%s", q.prefix, table)
}

// getGlobalQueueKey returns the Redis key for the global queue (all tables).
func (q *RedisQueue) getGlobalQueueKey() string {
	return fmt.Sprintf("%s:global", q.prefix)
}

// Enqueue adds a write operation to the queue.
// Operations are serialized as JSON and pushed to a Redis List.
func (q *RedisQueue) Enqueue(ctx context.Context, operation *core.WriteOperation) error {
	if q.closed {
		return ErrQueueClosed
	}

	if operation == nil {
		return ErrInvalidOperation
	}

	if operation.Table == "" {
		return fmt.Errorf("%w: table name is required", ErrInvalidOperation)
	}

	// Set timestamp if not set
	if operation.Timestamp.IsZero() {
		operation.Timestamp = time.Now()
	}

	// Serialize the operation
	opData, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("failed to marshal write operation: %w", err)
	}

	// If Redis list operations are available, use them for efficient queueing
	if q.ops != nil {
		// Push to both table-specific queue and global queue
		// Table-specific queue allows for table-based processing
		tableQueueKey := q.getQueueKey(operation.Table)
		if err := q.ops.ListPush(ctx, tableQueueKey, opData); err != nil {
			return fmt.Errorf("failed to enqueue operation to table queue: %w", err)
		}

		// Also push to global queue for unified processing
		globalQueueKey := q.getGlobalQueueKey()
		if err := q.ops.ListPush(ctx, globalQueueKey, opData); err != nil {
			// If global queue push fails, we still have it in the table queue
			// Log error but don't fail the operation
			return fmt.Errorf("failed to enqueue operation to global queue: %w", err)
		}
		return nil
	}

	// Fallback: use key-value storage with timestamp-based ordering
	// This is less efficient but works with any KVStore implementation
	opID := q.generateOperationID(operation)
	opKey := fmt.Sprintf("%s:op:%s:%s", q.prefix, operation.Table, opID)

	// Store the operation data
	if err := q.kvStore.Set(ctx, opKey, opData, 7*24*time.Hour); err != nil {
		return fmt.Errorf("failed to store operation: %w", err)
	}

	// Store operation metadata in a sorted way using timestamp-based keys
	queueIndexKey := fmt.Sprintf("%s:index:%s:%d:%s", q.prefix, operation.Table, operation.Timestamp.UnixNano(), opID)
	if err := q.kvStore.Set(ctx, queueIndexKey, []byte(opID), 7*24*time.Hour); err != nil {
		return fmt.Errorf("failed to store queue index: %w", err)
	}

	return nil
}

// Dequeue retrieves a batch of write operations from the queue.
// Returns operations in the order they were enqueued (FIFO).
// This method dequeues from the global queue. For table-specific dequeuing,
// use DequeueFromTable.
func (q *RedisQueue) Dequeue(ctx context.Context, batchSize int) ([]*core.WriteOperation, error) {
	if q.closed {
		return nil, ErrQueueClosed
	}

	if batchSize <= 0 {
		batchSize = 100
	}

	// If Redis list operations are available, use them for efficient dequeuing
	if q.ops != nil {
		// Dequeue from the global queue
		globalQueueKey := q.getGlobalQueueKey()
		operations := make([]*core.WriteOperation, 0, batchSize)

		for i := 0; i < batchSize; i++ {
			data, err := q.ops.ListPop(ctx, globalQueueKey)
			if err != nil {
				break
			}
			if data == nil {
				// Queue is empty
				break
			}

			var op core.WriteOperation
			if err := json.Unmarshal(data, &op); err != nil {
				// Skip invalid operations
				continue
			}

			operations = append(operations, &op)
		}

		return operations, nil
	}

	// Fallback: without list operations, we can't efficiently implement FIFO dequeue
	return nil, ErrRedisOperationsNotSupported
}

// DequeueFromTable retrieves a batch of write operations from a specific table's queue.
// This is a helper method for table-specific dequeuing when using Redis Lists.
func (q *RedisQueue) DequeueFromTable(ctx context.Context, table string, batchSize int) ([]*core.WriteOperation, error) {
	if q.closed {
		return nil, ErrQueueClosed
	}

	if batchSize <= 0 {
		batchSize = 100
	}

	if q.ops == nil {
		return nil, ErrRedisOperationsNotSupported
	}

	queueKey := q.getQueueKey(table)
	operations := make([]*core.WriteOperation, 0, batchSize)

	for i := 0; i < batchSize; i++ {
		data, err := q.ops.ListPop(ctx, queueKey)
		if err != nil {
			break
		}
		if data == nil {
			// Queue is empty
			break
		}

		var op core.WriteOperation
		if err := json.Unmarshal(data, &op); err != nil {
			// Skip invalid operations
			continue
		}

		operations = append(operations, &op)
	}

	return operations, nil
}

// Size returns the current number of operations in the global queue.
// For table-specific size, use SizeForTable.
func (q *RedisQueue) Size() int {
	if q.closed {
		return 0
	}

	// If Redis list operations are available, use them
	if q.ops != nil {
		ctx := context.Background()
		globalQueueKey := q.getGlobalQueueKey()
		length, err := q.ops.ListLength(ctx, globalQueueKey)
		if err != nil {
			return 0
		}
		return int(length)
	}

	// Without list operations, we can't efficiently count
	return 0
}

// SizeForTable returns the current number of operations in a specific table's queue.
func (q *RedisQueue) SizeForTable(table string) int {
	if q.closed {
		return 0
	}

	// If Redis list operations are available, use them
	if q.ops != nil {
		ctx := context.Background()
		queueKey := q.getQueueKey(table)
		length, err := q.ops.ListLength(ctx, queueKey)
		if err != nil {
			return 0
		}
		return int(length)
	}

	// Without list operations, we can't efficiently count
	return 0
}

// Close closes the queue.
func (q *RedisQueue) Close() error {
	q.closed = true
	return nil
}

// generateOperationID generates a unique operation ID.
func (q *RedisQueue) generateOperationID(operation *core.WriteOperation) string {
	return fmt.Sprintf("%s_%v_%d", operation.Table, operation.Key, time.Now().UnixNano())
}
