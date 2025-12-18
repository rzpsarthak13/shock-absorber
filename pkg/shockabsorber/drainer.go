package shockabsorber

import (
	"context"
	"log"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// Drainer handles background write-back operations from queue to database.
// It reads operations from the WriteBackQueue and writes them to the database
// at a controlled rate to protect the database from being overwhelmed.
type Drainer struct {
	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	tableName string
	queue     core.WriteBackQueue
	table     WriteBackExecutor
	config    DrainerConfig
}

// WriteBackExecutor is the interface for executing write-back operations.
// This is implemented by the internal table implementation.
type WriteBackExecutor interface {
	ExecuteWriteOperation(ctx context.Context, operation *core.WriteOperation) error
	GetWriteBackQueue() core.WriteBackQueue
}

// DrainerConfig contains configuration for the drainer.
type DrainerConfig struct {
	// DrainRate is the maximum number of DB writes per second.
	// This controls how fast operations are drained from the queue to the database.
	// Example: DrainRate=100 means 100 DB writes per second (1 write every 10ms).
	DrainRate int

	// BatchSize is how many operations to dequeue at once.
	// Currently set to 1 for precise rate limiting.
	BatchSize int

	// PollInterval is how often to check for new items when queue is empty.
	PollInterval time.Duration

	// MaxRetries is the maximum number of retries for failed operations.
	MaxRetries int

	// RetryBackoff is the base duration for exponential backoff retries.
	RetryBackoff time.Duration
}

// DefaultDrainerConfig returns sensible defaults for the drainer.
func DefaultDrainerConfig() DrainerConfig {
	return DrainerConfig{
		DrainRate:    50, // 50 DB writes per second
		BatchSize:    1,  // Process one at a time for precise rate limiting
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
	}
}

// NewDrainer creates a new drainer instance.
// The drainer will read from the queue and write to the database via the executor.
func NewDrainer(tableName string, queue core.WriteBackQueue, executor WriteBackExecutor, config DrainerConfig) *Drainer {
	if config.DrainRate <= 0 {
		config.DrainRate = DefaultDrainerConfig().DrainRate
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultDrainerConfig().BatchSize
	}
	if config.PollInterval <= 0 {
		config.PollInterval = DefaultDrainerConfig().PollInterval
	}

	return &Drainer{
		tableName: tableName,
		queue:     queue,
		table:     executor,
		config:    config,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the drainer goroutine.
// This is non-blocking - the drainer runs in a separate goroutine.
// Call Stop() to gracefully shut down the drainer.
func (d *Drainer) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		log.Printf("[DRAINER:%s] Already running", d.tableName)
		return nil
	}
	d.running = true
	// Reset channels for restart capability
	d.stopCh = make(chan struct{})
	d.doneCh = make(chan struct{})
	d.mu.Unlock()

	go d.run(ctx)
	log.Printf("[DRAINER:%s] Started with drain rate: %d ops/sec", d.tableName, d.config.DrainRate)
	return nil
}

// Stop gracefully stops the drainer.
// It waits for the current operation to complete before returning.
func (d *Drainer) Stop() error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = false
	d.mu.Unlock()

	log.Printf("[DRAINER:%s] Stopping...", d.tableName)
	close(d.stopCh)
	<-d.doneCh // Wait for goroutine to finish
	log.Printf("[DRAINER:%s] Stopped", d.tableName)
	return nil
}

// IsRunning returns whether the drainer is currently running.
func (d *Drainer) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// QueueSize returns the current size of the write-back queue.
func (d *Drainer) QueueSize() int {
	if d.queue == nil {
		return 0
	}
	return d.queue.Size()
}

// GetConfig returns the drainer configuration.
func (d *Drainer) GetConfig() DrainerConfig {
	return d.config
}

// run is the main drainer loop.
// It continuously reads from the queue and writes to the database at the configured rate.
func (d *Drainer) run(ctx context.Context) {
	defer close(d.doneCh)

	// Create rate limiter: DrainRate tokens per second
	// Token interval = 1 second / DrainRate
	// Example: DrainRate=100 → 1 token every 10ms
	limiter := rate.NewLimiter(rate.Limit(d.config.DrainRate), 1)

	log.Printf("[DRAINER:%s] Worker started - Rate: %d ops/sec, Poll interval: %v",
		d.tableName, d.config.DrainRate, d.config.PollInterval)

	operationCount := 0
	startTime := time.Now()

	for {
		select {
		case <-d.stopCh:
			log.Printf("[DRAINER:%s] Received stop signal, processed %d operations in %v",
				d.tableName, operationCount, time.Since(startTime))
			return
		case <-ctx.Done():
			log.Printf("[DRAINER:%s] Context cancelled, processed %d operations in %v",
				d.tableName, operationCount, time.Since(startTime))
			return
		default:
			// Check if queue has items
			queueSize := d.queue.Size()
			if queueSize == 0 {
				// No work to do, sleep briefly and check again
				time.Sleep(d.config.PollInterval)
				continue
			}

			// Dequeue operation(s) first
			operations, err := d.queue.Dequeue(ctx, d.config.BatchSize)
			if err != nil {
				log.Printf("[DRAINER:%s] Dequeue error: %v", d.tableName, err)
				continue
			}

			if len(operations) == 0 {
				continue
			}

			// Process each operation - enforce rate limit for EACH operation
			for _, op := range operations {
				if op == nil {
					continue
				}

				// Wait for rate limiter BEFORE processing each operation
				// This ensures exact rate limiting: 1 operation per (1/DrainRate) seconds
				if err := limiter.Wait(ctx); err != nil {
					if err == context.Canceled || err == context.DeadlineExceeded {
						return
					}
					log.Printf("[DRAINER:%s] Rate limiter error: %v", d.tableName, err)
					continue
				}

				operationCount++
				timestamp := time.Now().Format("2006-01-02 15:04:05.000")

				log.Printf("[DRAINER:%s] [%s] Processing operation #%d: %s on key %v (queue size: %d)",
					d.tableName, timestamp, operationCount, op.Operation, op.Key, d.queue.Size())

				// Execute the write operation to the database
				writeStart := time.Now()
				if err := d.table.ExecuteWriteOperation(ctx, op); err != nil {
					log.Printf("[DRAINER:%s] [%s] ERROR: Failed to write to DB: %v (duration: %v)",
						d.tableName, time.Now().Format("2006-01-02 15:04:05.000"), err, time.Since(writeStart))
					// TODO: Implement retry logic with DLQ
					continue
				}

				log.Printf("[DRAINER:%s] [%s] ✓ Successfully written to DB (duration: %v)",
					d.tableName, time.Now().Format("2006-01-02 15:04:05.000"), time.Since(writeStart))
			}
		}
	}
}

// DrainerManager manages multiple drainers (one per table).
type DrainerManager struct {
	mu       sync.RWMutex
	drainers map[string]*Drainer
	config   DrainerConfig
}

// NewDrainerManager creates a new drainer manager.
func NewDrainerManager(config DrainerConfig) *DrainerManager {
	return &DrainerManager{
		drainers: make(map[string]*Drainer),
		config:   config,
	}
}

// AddDrainer adds a drainer for a table.
func (dm *DrainerManager) AddDrainer(tableName string, queue core.WriteBackQueue, executor WriteBackExecutor) *Drainer {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Check if drainer already exists
	if existing, ok := dm.drainers[tableName]; ok {
		return existing
	}

	drainer := NewDrainer(tableName, queue, executor, dm.config)
	dm.drainers[tableName] = drainer
	return drainer
}

// GetDrainer returns the drainer for a table.
func (dm *DrainerManager) GetDrainer(tableName string) *Drainer {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.drainers[tableName]
}

// StartAll starts all drainers.
func (dm *DrainerManager) StartAll(ctx context.Context) error {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	for _, drainer := range dm.drainers {
		if err := drainer.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all drainers.
func (dm *DrainerManager) StopAll() error {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	for _, drainer := range dm.drainers {
		if err := drainer.Stop(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveDrainer removes and stops a drainer for a table.
func (dm *DrainerManager) RemoveDrainer(tableName string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	drainer, ok := dm.drainers[tableName]
	if !ok {
		return nil
	}

	if err := drainer.Stop(); err != nil {
		return err
	}

	delete(dm.drainers, tableName)
	return nil
}

// Count returns the number of drainers.
func (dm *DrainerManager) Count() int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return len(dm.drainers)
}
