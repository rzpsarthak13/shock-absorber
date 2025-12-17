package shockabsorber

import (
	"context"
	"fmt"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/client"
	"gopkg.in/yaml.v3"
)

// Client is the main interface for interacting with the shock absorber system.
// It provides methods to enable/disable KV operations for tables and to retrieve
// table instances for performing CRUD operations.
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

	// Close closes all connections and releases resources.
	// This should be called when the client is no longer needed.
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
	impl   *client.ClientImpl
	config *Config
}

// NewClient creates a new shock absorber client with the provided configuration.
// The client will initialize connections to the KV store and database based on
// the configuration. Returns an error if initialization fails.
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

	return &clientWrapper{impl: impl, config: config}, nil
}

// EnableKV enables KV store caching for a table.
func (cw *clientWrapper) EnableKV(ctx context.Context, tableName string, opts ...TableOption) (Table, error) {
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

	table, err := cw.impl.EnableKV(ctx, tableName, tableConfigData)
	if err != nil {
		return nil, err
	}
	return &tableWrapper{table: table}, nil
}

// DisableKV disables KV store caching for a table.
func (cw *clientWrapper) DisableKV(ctx context.Context, tableName string) error {
	return cw.impl.DisableKV(ctx, tableName)
}

// GetTable retrieves a table instance by name.
func (cw *clientWrapper) GetTable(tableName string) (Table, error) {
	table, err := cw.impl.GetTable(tableName)
	if err != nil {
		return nil, err
	}
	return &tableWrapper{table: table}, nil
}

// Close closes all connections and releases resources.
func (cw *clientWrapper) Close() error {
	return cw.impl.Close()
}

// GetInternalTable is a helper method for testing/debugging that returns the internal core.Table.
// This should only be used in test code, not in production.
func (cw *clientWrapper) GetInternalTable(tableName string) (interface{}, error) {
	return cw.impl.GetTable(tableName)
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
