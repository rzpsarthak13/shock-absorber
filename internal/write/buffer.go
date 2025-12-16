package write

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/razorpay/shock-absorber/internal/core"
)

var (
	// ErrBufferFull is returned when the buffer is full and cannot accept more writes.
	ErrBufferFull = errors.New("write buffer is full")

	// ErrBufferClosed is returned when trying to write to a closed buffer.
	ErrBufferClosed = errors.New("write buffer is closed")
)

// BufferedWrite represents a buffered write operation.
type BufferedWrite struct {
	// Key is the KV store key.
	Key string

	// Value is the serialized value.
	Value []byte

	// TTL is the time-to-live for the key.
	TTL time.Duration

	// Operation is the WAL operation associated with this write.
	Operation *WALOperation
}

// WriteBuffer buffers writes in memory before flushing them to Redis in batches.
// This improves efficiency by reducing the number of network round-trips.
type WriteBuffer struct {
	kvStore    core.KVStore
	walManager *WALManager

	// Buffer configuration
	maxSize      int
	flushSize    int
	flushTimeout time.Duration

	// Internal state
	buffer  []*BufferedWrite
	mu      sync.Mutex
	closed  bool
	flushCh chan struct{}
	stopCh  chan struct{}
	wg      sync.WaitGroup

	// Metrics
	pendingWrites int64
	totalFlushes  int64
	totalWrites   int64
}

// WriteBufferConfig contains configuration for the write buffer.
type WriteBufferConfig struct {
	// MaxSize is the maximum number of writes to buffer before forcing a flush.
	MaxSize int

	// FlushSize is the number of writes to accumulate before auto-flushing.
	FlushSize int

	// FlushTimeout is the maximum time to wait before flushing the buffer,
	// even if FlushSize hasn't been reached.
	FlushTimeout time.Duration
}

// DefaultWriteBufferConfig returns a configuration with sensible defaults.
func DefaultWriteBufferConfig() WriteBufferConfig {
	return WriteBufferConfig{
		MaxSize:      1000,
		FlushSize:    100,
		FlushTimeout: 100 * time.Millisecond,
	}
}

// NewWriteBuffer creates a new write buffer.
func NewWriteBuffer(kvStore core.KVStore, walManager *WALManager, config WriteBufferConfig) *WriteBuffer {
	if config.MaxSize == 0 {
		config = DefaultWriteBufferConfig()
	}

	buf := &WriteBuffer{
		kvStore:      kvStore,
		walManager:   walManager,
		maxSize:      config.MaxSize,
		flushSize:    config.FlushSize,
		flushTimeout: config.FlushTimeout,
		buffer:       make([]*BufferedWrite, 0, config.FlushSize),
		flushCh:      make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
	}

	// Start the background flusher
	buf.wg.Add(1)
	go buf.flusher()

	return buf
}

// Write adds a write operation to the buffer.
// Returns ErrBufferFull if the buffer is at capacity and ErrBufferClosed if closed.
func (b *WriteBuffer) Write(ctx context.Context, write *BufferedWrite) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrBufferClosed
	}

	if len(b.buffer) >= b.maxSize {
		return ErrBufferFull
	}

	b.buffer = append(b.buffer, write)
	b.totalWrites++

	// Trigger flush if we've reached the flush size
	if len(b.buffer) >= b.flushSize {
		select {
		case b.flushCh <- struct{}{}:
		default:
		}
	}

	return nil
}

// Flush immediately flushes all buffered writes to Redis.
func (b *WriteBuffer) Flush(ctx context.Context) error {
	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return nil
	}

	// Copy the buffer and clear it
	writes := make([]*BufferedWrite, len(b.buffer))
	copy(writes, b.buffer)
	b.buffer = b.buffer[:0]
	b.mu.Unlock()

	return b.flushWrites(ctx, writes)
}

// flushWrites flushes a batch of writes to Redis.
func (b *WriteBuffer) flushWrites(ctx context.Context, writes []*BufferedWrite) error {
	if len(writes) == 0 {
		return nil
	}

	// Group writes by TTL for batch operations
	// For simplicity, we'll use the first TTL if all are the same, otherwise do individual writes
	ttlGroups := make(map[time.Duration][]*BufferedWrite)
	for _, write := range writes {
		ttlGroups[write.TTL] = append(ttlGroups[write.TTL], write)
	}

	// Flush each TTL group
	for ttl, group := range ttlGroups {
		items := make(map[string][]byte)
		var walOps []*WALOperation

		for _, write := range group {
			items[write.Key] = write.Value
			if write.Operation != nil {
				walOps = append(walOps, write.Operation)
			}
		}

		// Batch set to Redis
		if err := b.kvStore.BatchSet(ctx, items, ttl); err != nil {
			// If batch fails, try individual writes
			for _, write := range group {
				if err := b.kvStore.Set(ctx, write.Key, write.Value, write.TTL); err != nil {
					// Log error but continue with other writes
					// In production, you might want to return this error or handle it differently
					continue
				}
			}
		}

		// Append to WAL
		for _, op := range walOps {
			if err := b.walManager.Append(ctx, op); err != nil {
				// Log error but continue
				// WAL failures should be handled but shouldn't block the write
				continue
			}
		}
	}

	b.mu.Lock()
	b.totalFlushes++
	b.mu.Unlock()

	return nil
}

// flusher is a background goroutine that periodically flushes the buffer.
func (b *WriteBuffer) flusher() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			// Final flush on shutdown
			ctx := context.Background()
			_ = b.Flush(ctx)
			return

		case <-ticker.C:
			// Periodic flush
			ctx := context.Background()
			_ = b.Flush(ctx)

		case <-b.flushCh:
			// Flush triggered by buffer size
			ctx := context.Background()
			_ = b.Flush(ctx)
		}
	}
}

// Close closes the buffer and flushes any remaining writes.
func (b *WriteBuffer) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	close(b.stopCh)
	b.wg.Wait()

	// Final flush
	ctx := context.Background()
	return b.Flush(ctx)
}

// Size returns the current number of buffered writes.
func (b *WriteBuffer) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buffer)
}

// Stats returns buffer statistics.
func (b *WriteBuffer) Stats() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	return map[string]interface{}{
		"buffer_size":    len(b.buffer),
		"pending_writes": b.pendingWrites,
		"total_flushes":  b.totalFlushes,
		"total_writes":   b.totalWrites,
		"closed":         b.closed,
	}
}
