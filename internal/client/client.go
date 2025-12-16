package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/razorpay/shock-absorber/internal/core"
	"github.com/razorpay/shock-absorber/internal/database"
	"github.com/razorpay/shock-absorber/internal/kvstore"
	"github.com/razorpay/shock-absorber/internal/registry"
	"github.com/razorpay/shock-absorber/internal/table"
	"github.com/razorpay/shock-absorber/internal/writeback"
)

// TableConfigData represents table configuration data without importing the public package.
type TableConfigData struct {
	TTL                time.Duration
	WriteBackBatchSize int
	DrainRate          int
	Enabled            bool
	Namespace          string
}

// ClientImpl is the default implementation of the Client interface.
type ClientImpl struct {
	mu            sync.RWMutex
	configMgr     *registry.ConfigManager
	kvStore       core.KVStore
	database      core.Database
	tableRegistry *registry.TableRegistry
	lifecycle     *registry.LifecycleManager
	closed        bool
}

// ConfigProvider is an interface to provide configuration as YAML without importing the public package.
type ConfigProvider interface {
	GetYAML() ([]byte, error)
}

// NewClientImpl creates a new shock absorber client implementation.
// It accepts a config provider to avoid import cycles.
func NewClientImpl(configProvider ConfigProvider) (*ClientImpl, error) {
	if configProvider == nil {
		return nil, fmt.Errorf("config provider cannot be nil")
	}

	// Create config manager and load config from YAML
	configMgr := registry.NewConfigManager()
	yamlData, err := configProvider.GetYAML()
	if err != nil {
		return nil, fmt.Errorf("failed to get config YAML: %w", err)
	}
	if err := configMgr.LoadFromYAML(yamlData); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create lifecycle manager
	lifecycle := registry.NewLifecycleManager()

	// Create table registry
	tableRegistry := registry.NewTableRegistry(configMgr, lifecycle)

	client := &ClientImpl{
		tableRegistry: tableRegistry,
		configMgr:     configMgr,
		lifecycle:     lifecycle,
		closed:        false,
	}

	// Initialize KV store and database connections
	// Note: These implementations may not exist yet, so we'll create placeholders
	// that can be set later or will be implemented in future phases
	if err := client.initializeConnections(); err != nil {
		return nil, fmt.Errorf("failed to initialize connections: %w", err)
	}

	return client, nil
}

// initializeConnections initializes the KV store and database connections.
func (c *ClientImpl) initializeConnections() error {
	config := c.configMgr.GetConfig()

	// Initialize KV store
	kvStore, err := kvstore.NewRedisKVStore(
		config.KVStore.Endpoints,
		config.KVStore.Password,
		config.KVStore.DB,
		config.KVStore.PoolSize,
		config.KVStore.MinIdleConns,
		config.KVStore.DialTimeout,
		config.KVStore.ReadTimeout,
		config.KVStore.WriteTimeout,
	)
	if err != nil {
		return fmt.Errorf("failed to create KV store: %w", err)
	}
	c.kvStore = kvStore

	// Initialize database
	if config.Database.Type == "mysql" {
		db, err := database.NewMySQLDatabase(
			config.Database.Host,
			config.Database.Port,
			config.Database.Database,
			config.Database.Username,
			config.Database.Password,
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
			config.Database.ConnectionTimeout,
		)
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
		c.database = db
	} else {
		return fmt.Errorf("unsupported database type: %s", config.Database.Type)
	}

	return nil
}

// EnableKV enables KV store caching for a table.
func (c *ClientImpl) EnableKV(ctx context.Context, tableName string, tableConfig TableConfigData) (core.Table, error) {
	if tableName == "" {
		return nil, fmt.Errorf("table name cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("client is closed")
	}

	// Check if table is already registered
	existingTable, err := c.tableRegistry.Get(tableName)
	if err == nil {
		// Table exists, check if already enabled
		metadata, _ := c.tableRegistry.GetMetadata(tableName)
		if metadata != nil && metadata.Enabled {
			// Already enabled, return the table
			return existingTable, nil
		}
		// Table exists but not enabled, enable it
		if err := c.tableRegistry.EnableKV(ctx, tableName); err != nil {
			return nil, fmt.Errorf("failed to enable KV for table %q: %w", tableName, err)
		}
		return existingTable, nil
	}

	// Table not registered, need to:
	// 1. Discover schema from database
	// 2. Create table instance
	// 3. Register table
	// 4. Enable KV

	// Discover schema
	if c.database == nil {
		return nil, fmt.Errorf("database connection not initialized")
	}

	schema, err := c.database.GetSchema(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover schema for table %q: %w", tableName, err)
	}

	// Create table instance
	// Note: This will need to be implemented when table implementations are available
	// For now, we'll return an error indicating this needs to be implemented
	// In a real implementation, this would create a table instance that wraps
	// the KV store, database, schema translator, etc.
	table, err := c.createTableInstance(tableName, schema, tableConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create table instance: %w", err)
	}

	// Register table
	if err := c.tableRegistry.Register(ctx, tableName, table, schema); err != nil {
		return nil, fmt.Errorf("failed to register table: %w", err)
	}

	// Enable KV
	if err := c.tableRegistry.EnableKV(ctx, tableName); err != nil {
		return nil, fmt.Errorf("failed to enable KV for table %q: %w", tableName, err)
	}

	return table, nil
}

// createTableInstance creates a table instance for the given table name and schema.
func (c *ClientImpl) createTableInstance(tableName string, schema *core.Schema, config TableConfigData) (core.Table, error) {
	if c.kvStore == nil {
		return nil, fmt.Errorf("KV store not initialized")
	}
	if c.database == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// Create write-back queue
	var queue core.WriteBackQueue
	internalConfig := c.configMgr.GetConfig()
	switch internalConfig.WriteBack.QueueType {
	case "memory":
		queue = writeback.NewMemoryQueue(internalConfig.WriteBack.QueueBufferSize)
	case "redis":
		redisKVStore, ok := c.kvStore.(*kvstore.RedisKVStore)
		if !ok {
			return nil, fmt.Errorf("Redis KV store required for Redis queue type")
		}
		queue = writeback.NewRedisQueue(redisKVStore, "wbq")
	case "kafka":
		kafkaConfig := writeback.KafkaQueueConfig{
			Brokers:         internalConfig.WriteBack.KafkaConfig.Brokers,
			Topic:           internalConfig.WriteBack.KafkaConfig.Topic,
			GroupID:         internalConfig.WriteBack.KafkaConfig.GroupID,
			BatchSize:       internalConfig.WriteBack.KafkaConfig.BatchSize,
			BatchTimeout:    internalConfig.WriteBack.KafkaConfig.BatchTimeout,
			WriteTimeout:    internalConfig.WriteBack.KafkaConfig.WriteTimeout,
			ReadTimeout:     internalConfig.WriteBack.KafkaConfig.ReadTimeout,
			RequiredAcks:    internalConfig.WriteBack.KafkaConfig.RequiredAcks,
			MaxMessageBytes: internalConfig.WriteBack.KafkaConfig.MaxMessageBytes,
			MinBytes:        internalConfig.WriteBack.KafkaConfig.MinBytes,
			MaxBytes:        internalConfig.WriteBack.KafkaConfig.MaxBytes,
			MaxWait:         internalConfig.WriteBack.KafkaConfig.MaxWait,
		}
		var err error
		queue, err = writeback.NewKafkaQueue(kafkaConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Kafka queue: %w", err)
		}
	default:
		// Default to memory queue
		queue = writeback.NewMemoryQueue(internalConfig.WriteBack.QueueBufferSize)
	}

	// Create table instance
	tableInstance := table.NewTableImpl(
		tableName,
		schema,
		c.kvStore,
		c.database,
		queue,
		config.TTL,
		config.Namespace,
	)

	return tableInstance, nil
}

// DisableKV disables KV store caching for a table.
func (c *ClientImpl) DisableKV(ctx context.Context, tableName string) error {
	if tableName == "" {
		return fmt.Errorf("table name cannot be empty")
	}

	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()

	if closed {
		return fmt.Errorf("client is closed")
	}

	return c.tableRegistry.DisableKV(ctx, tableName)
}

// GetTable retrieves a table instance by name.
func (c *ClientImpl) GetTable(tableName string) (core.Table, error) {
	if tableName == "" {
		return nil, fmt.Errorf("table name cannot be empty")
	}

	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()

	if closed {
		return nil, fmt.Errorf("client is closed")
	}

	return c.tableRegistry.Get(tableName)
}

// Close closes all connections and releases resources.
func (c *ClientImpl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	var errs []error

	// Close all tables
	ctx := context.Background()
	if err := c.tableRegistry.Clear(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to clear table registry: %w", err))
	}

	// Close KV store
	if c.kvStore != nil {
		if err := c.kvStore.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close KV store: %w", err))
		}
	}

	// Close database
	if c.database != nil {
		if err := c.database.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	return nil
}
