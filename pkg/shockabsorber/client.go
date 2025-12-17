package shockabsorber

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/client"
	"github.com/rzpsarthak13/shock-absorber/internal/core"
	"gopkg.in/yaml.v3"
)

// Client is the main interface for interacting with the shock absorber system.
// It provides methods to enable/disable KV operations for tables and to retrieve
// table instances for performing CRUD operations.
//
// Typical usage:
//
//	client, _ := shockabsorber.NewClient(config)
//	defer client.Close()
//
//	table, _ := client.EnableKV(ctx, "payments")
//	client.Start(ctx)  // Start background drainer
//	defer client.Stop()
//
//	table.Create(ctx, record)
//	table.Read(ctx, key)
type Client interface {
	// EnableKV enables KV store caching for a table.
	// The table must exist in the database and will be automatically registered
	// if not already registered. Returns a Table instance that can be used for operations.
	// Options can be provided to override default table configuration.
	EnableKV(ctx context.Context, tableName string, opts ...TableOption) (Table, error)

	// DisableKV disables KV store caching for a table.
	// After disabling, operations will go directly to the database.
	DisableKV(ctx context.Context, tableName string) error

	// GetTable retrieves a table instance by name.
	// The table must be registered (via EnableKV) before it can be retrieved.
	// Returns an error if the table is not registered.
	GetTable(tableName string) (Table, error)

	// Start starts the background drainer workers for all enabled tables.
	// The drainers read from the write-back queue and write to the database
	// at the configured drain rate. This should be called after EnableKV.
	// This is non-blocking - drainers run in separate goroutines.
	Start(ctx context.Context) error

	// Stop gracefully stops all background drainer workers.
	// It waits for current operations to complete before returning.
	// This should be called before Close().
	Stop() error

	// IsRunning returns whether the drainer workers are currently running.
	IsRunning() bool

	// Close closes all connections and releases resources.
	// This should be called when the client is no longer needed.
	// It will also stop any running drainers.
	Close() error
}

// configProvider implements client.ConfigProvider to provide config as YAML without import cycles.
type configProvider struct {
	config *Config
}

func (cp *configProvider) GetYAML() ([]byte, error) {
	return yaml.Marshal(cp.config)
}

// clientWrapper wraps the internal client implementation to provide the public Client interface.
type clientWrapper struct {
	mu             sync.RWMutex
	impl           *client.ClientImpl
	config         *Config
	drainerManager *DrainerManager
	tables         map[string]*tableWrapper // Track enabled tables for drainer setup
	started        bool
}

// NewClient creates a new shock absorber client with the provided configuration.
// The client will initialize connections to the KV store and database based on
// the configuration. Returns an error if initialization fails.
//
// After creating the client:
// 1. Call EnableKV() for each table you want to use
// 2. Call Start() to begin background write-back processing
// 3. Use the returned Table instances for CRUD operations
// 4. Call Stop() and Close() when done
func NewClient(config *Config) (Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Create config provider to avoid import cycles
	configProv := &configProvider{config: config}

	impl, err := client.NewClientImpl(configProv)
	if err != nil {
		return nil, err
	}

	// Create drainer manager with config from WriteBack settings
	drainerConfig := DrainerConfig{
		DrainRate:    config.WriteBack.DrainRate,
		BatchSize:    1, // Process one at a time for precise rate limiting
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   config.WriteBack.MaxRetries,
		RetryBackoff: config.WriteBack.RetryBackoffBase,
	}

	return &clientWrapper{
		impl:           impl,
		config:         config,
		drainerManager: NewDrainerManager(drainerConfig),
		tables:         make(map[string]*tableWrapper),
		started:        false,
	}, nil
}

// EnableKV enables KV store caching for a table.
func (cw *clientWrapper) EnableKV(ctx context.Context, tableName string, opts ...TableOption) (Table, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// Get base table config from the config
	tableConfig := cw.config.Tables[tableName]
	// Apply defaults if not set
	if tableConfig.TTL == 0 {
		tableConfig.TTL = cw.config.WriteBack.DefaultTTL
	}
	if tableConfig.WriteBackBatchSize == 0 {
		tableConfig.WriteBackBatchSize = cw.config.WriteBack.BatchSize
	}
	if tableConfig.DrainRate == 0 {
		tableConfig.DrainRate = cw.config.WriteBack.DrainRate
	}

	// Apply options
	for _, opt := range opts {
		opt(&tableConfig)
	}

	// Convert to internal TableConfigData
	tableConfigData := client.TableConfigData{
		TTL:                tableConfig.TTL,
		WriteBackBatchSize: tableConfig.WriteBackBatchSize,
		DrainRate:          tableConfig.DrainRate,
		Enabled:            tableConfig.Enabled,
		Namespace:          tableConfig.Namespace,
	}

	internalTable, err := cw.impl.EnableKV(ctx, tableName, tableConfigData)
	if err != nil {
		return nil, err
	}

	// Create table wrapper
	tw := &tableWrapper{table: internalTable}
	cw.tables[tableName] = tw

	// Get the write-back queue from the internal table
	queue := internalTable.GetWriteBackQueue()
	if queue == nil {
		return nil, fmt.Errorf("table %s does not have a write-back queue", tableName)
	}

	// Create a drainer for this table with table-specific config
	drainerConfig := DrainerConfig{
		DrainRate:    tableConfig.DrainRate,
		BatchSize:    1,
		PollInterval: 100 * time.Millisecond,
		MaxRetries:   cw.config.WriteBack.MaxRetries,
		RetryBackoff: cw.config.WriteBack.RetryBackoffBase,
	}

	// Create executor wrapper that implements WriteBackExecutor
	executor := &tableExecutor{table: internalTable}

	// Add drainer (but don't start yet - Start() will do that)
	drainer := NewDrainer(tableName, queue, executor, drainerConfig)
	cw.drainerManager.drainers[tableName] = drainer

	// If client is already started, start this drainer too
	if cw.started {
		if err := drainer.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start drainer for table %s: %w", tableName, err)
		}
	}

	return tw, nil
}

// tableExecutor wraps core.Table to implement WriteBackExecutor
type tableExecutor struct {
	table core.Table
}

func (te *tableExecutor) ExecuteWriteOperation(ctx context.Context, operation *core.WriteOperation) error {
	return te.table.ExecuteWriteOperation(ctx, operation)
}

func (te *tableExecutor) GetWriteBackQueue() core.WriteBackQueue {
	return te.table.GetWriteBackQueue()
}

// DisableKV disables KV store caching for a table.
func (cw *clientWrapper) DisableKV(ctx context.Context, tableName string) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// Stop the drainer for this table
	if err := cw.drainerManager.RemoveDrainer(tableName); err != nil {
		return fmt.Errorf("failed to stop drainer for table %s: %w", tableName, err)
	}

	// Remove from tracked tables
	delete(cw.tables, tableName)

	return cw.impl.DisableKV(ctx, tableName)
}

// GetTable retrieves a table instance by name.
func (cw *clientWrapper) GetTable(tableName string) (Table, error) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()

	// Check if we have a cached wrapper
	if tw, ok := cw.tables[tableName]; ok {
		return tw, nil
	}

	// Fall back to impl
	table, err := cw.impl.GetTable(tableName)
	if err != nil {
		return nil, err
	}
	return &tableWrapper{table: table}, nil
}

// Start starts the background drainer workers for all enabled tables.
func (cw *clientWrapper) Start(ctx context.Context) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.started {
		return nil // Already started
	}

	if err := cw.drainerManager.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start drainers: %w", err)
	}

	cw.started = true
	return nil
}

// Stop gracefully stops all background drainer workers.
func (cw *clientWrapper) Stop() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.started {
		return nil // Not started
	}

	if err := cw.drainerManager.StopAll(); err != nil {
		return fmt.Errorf("failed to stop drainers: %w", err)
	}

	cw.started = false
	return nil
}

// IsRunning returns whether the drainer workers are currently running.
func (cw *clientWrapper) IsRunning() bool {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.started
}

// Close closes all connections and releases resources.
func (cw *clientWrapper) Close() error {
	// Stop drainers first
	if err := cw.Stop(); err != nil {
		// Log but continue with close
		fmt.Printf("Warning: error stopping drainers: %v\n", err)
	}

	return cw.impl.Close()
}

// GetInternalTable is a helper method for testing/debugging that returns the internal core.Table.
// This should only be used in test code, not in production.
func (cw *clientWrapper) GetInternalTable(tableName string) (interface{}, error) {
	return cw.impl.GetTable(tableName)
}

// GetDrainer returns the drainer for a specific table.
// This is useful for monitoring queue size and drainer status.
func (cw *clientWrapper) GetDrainer(tableName string) *Drainer {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.drainerManager.GetDrainer(tableName)
}

// TableOption is a function type for configuring table options.
type TableOption func(*TableConfig)

// WithTTL sets the TTL for cached records in the KV store.
func WithTTL(ttl time.Duration) TableOption {
	return func(config *TableConfig) {
		config.TTL = ttl
	}
}

// WithNamespace sets the namespace prefix for keys in the KV store.
func WithNamespace(namespace string) TableOption {
	return func(config *TableConfig) {
		config.Namespace = namespace
	}
}

// WithWriteBackBatchSize sets the batch size for write-back operations.
func WithWriteBackBatchSize(batchSize int) TableOption {
	return func(config *TableConfig) {
		config.WriteBackBatchSize = batchSize
	}
}

// WithDrainRate sets the maximum number of operations per second to drain.
func WithDrainRate(drainRate int) TableOption {
	return func(config *TableConfig) {
		config.DrainRate = drainRate
	}
}
