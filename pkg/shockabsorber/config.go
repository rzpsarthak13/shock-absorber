package shockabsorber

import (
	"time"
)

// Config represents the root configuration for the shock absorber client.
type Config struct {
	// KVStore contains configuration for the key-value store (Redis/ElastiCache).
	KVStore KVStoreConfig `yaml:"kvstore" json:"kvstore"`

	// Database contains configuration for the persistent database.
	Database DatabaseConfig `yaml:"database" json:"database"`

	// Tables contains table-specific configuration overrides.
	// If a table is not specified here, default settings will be used.
	Tables map[string]TableConfig `yaml:"tables,omitempty" json:"tables,omitempty"`

	// WriteBack contains global write-back processing configuration.
	WriteBack WriteBackConfig `yaml:"writeback" json:"writeback"`

	// AutoScaling contains auto-scaling configuration for drainers.
	AutoScaling AutoScalingConfig `yaml:"autoscaling" json:"autoscaling"`
}

// KVStoreConfig contains configuration for the key-value store.
type KVStoreConfig struct {
	// Type specifies the KV store type. Currently supports "redis" or "elasticache".
	Type string `yaml:"type" json:"type"`

	// Endpoints is a list of Redis/ElastiCache endpoints.
	// For single-node Redis, use a single endpoint.
	// For cluster mode, provide all cluster endpoints.
	Endpoints []string `yaml:"endpoints" json:"endpoints"`

	// ClusterMode indicates whether to use Redis cluster mode.
	ClusterMode bool `yaml:"cluster_mode" json:"cluster_mode"`

	// Password is the authentication password for Redis.
	Password string `yaml:"password,omitempty" json:"password,omitempty"`

	// DB is the Redis database number (0-15). Only used in non-cluster mode.
	DB int `yaml:"db,omitempty" json:"db,omitempty"`

	// MaxRetries is the maximum number of retries for failed operations.
	MaxRetries int `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`

	// PoolSize is the connection pool size per node.
	PoolSize int `yaml:"pool_size,omitempty" json:"pool_size,omitempty"`

	// MinIdleConns is the minimum number of idle connections in the pool.
	MinIdleConns int `yaml:"min_idle_conns,omitempty" json:"min_idle_conns,omitempty"`

	// DialTimeout is the timeout for establishing connections.
	DialTimeout time.Duration `yaml:"dial_timeout,omitempty" json:"dial_timeout,omitempty"`

	// ReadTimeout is the timeout for read operations.
	ReadTimeout time.Duration `yaml:"read_timeout,omitempty" json:"read_timeout,omitempty"`

	// WriteTimeout is the timeout for write operations.
	WriteTimeout time.Duration `yaml:"write_timeout,omitempty" json:"write_timeout,omitempty"`
}

// DatabaseConfig contains configuration for the persistent database.
type DatabaseConfig struct {
	// Type specifies the database type. Currently supports "mysql" or "postgresql".
	Type string `yaml:"type" json:"type"`

	// Host is the database host address.
	Host string `yaml:"host" json:"host"`

	// Port is the database port number.
	Port int `yaml:"port" json:"port"`

	// Database is the database name.
	Database string `yaml:"database" json:"database"`

	// Username is the database username.
	Username string `yaml:"username" json:"username"`

	// Password is the database password.
	Password string `yaml:"password,omitempty" json:"password,omitempty"`

	// SSLMode is the SSL mode for PostgreSQL (e.g., "require", "disable", "verify-full").
	// For MySQL, use SSL-related connection parameters.
	SSLMode string `yaml:"ssl_mode,omitempty" json:"ssl_mode,omitempty"`

	// MaxOpenConns is the maximum number of open connections to the database.
	MaxOpenConns int `yaml:"max_open_conns,omitempty" json:"max_open_conns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections in the pool.
	MaxIdleConns int `yaml:"max_idle_conns,omitempty" json:"max_idle_conns,omitempty"`

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime,omitempty" json:"conn_max_lifetime,omitempty"`

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle.
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time,omitempty" json:"conn_max_idle_time,omitempty"`

	// ConnectionTimeout is the timeout for establishing database connections.
	ConnectionTimeout time.Duration `yaml:"connection_timeout,omitempty" json:"connection_timeout,omitempty"`
}

// TableConfig contains table-specific configuration overrides.
type TableConfig struct {
	// TTL is the time-to-live for cached records in the KV store.
	// If not set, uses the default from WriteBackConfig.
	TTL time.Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`

	// WriteBackBatchSize is the batch size for write-back operations for this table.
	// If not set, uses the default from WriteBackConfig.
	WriteBackBatchSize int `yaml:"writeback_batch_size,omitempty" json:"writeback_batch_size,omitempty"`

	// DrainRate is the maximum number of operations per second to drain for this table.
	// If not set, uses the default from WriteBackConfig.
	DrainRate int `yaml:"drain_rate,omitempty" json:"drain_rate,omitempty"`

	// Enabled indicates whether KV store caching is enabled for this table.
	// Defaults to false.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Namespace is an optional namespace prefix for keys in the KV store.
	// Format: {namespace}:{table}:{primary_key_value}
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// WriteBackConfig contains global write-back processing configuration.
type WriteBackConfig struct {
	// BatchSize is the default batch size for write-back operations.
	BatchSize int `yaml:"batch_size" json:"batch_size"`

	// DrainRate is the default maximum number of operations per second to drain.
	DrainRate int `yaml:"drain_rate" json:"drain_rate"`

	// MaxRetries is the maximum number of retries for failed write-back operations.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`

	// RetryBackoffBase is the base duration for exponential backoff retries.
	RetryBackoffBase time.Duration `yaml:"retry_backoff_base" json:"retry_backoff_base"`

	// RetryBackoffMax is the maximum duration for exponential backoff retries.
	RetryBackoffMax time.Duration `yaml:"retry_backoff_max" json:"retry_backoff_max"`

	// DefaultTTL is the default time-to-live for cached records.
	DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

	// QueueType specifies the queue implementation type.
	// Options: "memory", "redis", "kafka" (default: "memory").
	QueueType string `yaml:"queue_type,omitempty" json:"queue_type,omitempty"`

	// QueueBufferSize is the buffer size for the in-memory queue.
	// Only used when QueueType is "memory".
	QueueBufferSize int `yaml:"queue_buffer_size,omitempty" json:"queue_buffer_size,omitempty"`

	// KafkaConfig contains Kafka-specific configuration.
	// Only used when QueueType is "kafka".
	KafkaConfig KafkaConfig `yaml:"kafka_config,omitempty" json:"kafka_config,omitempty"`
}

// KafkaConfig contains configuration for Kafka queue.
type KafkaConfig struct {
	// Brokers is a list of Kafka broker addresses (e.g., ["localhost:9092"]).
	Brokers []string `yaml:"brokers" json:"brokers"`

	// Topic is the Kafka topic name for write-back operations.
	Topic string `yaml:"topic" json:"topic"`

	// GroupID is the consumer group ID for reading from Kafka.
	GroupID string `yaml:"group_id" json:"group_id"`

	// BatchSize is the batch size for Kafka producer.
	BatchSize int `yaml:"batch_size" json:"batch_size"`

	// BatchTimeout is the timeout for batching messages.
	BatchTimeout time.Duration `yaml:"batch_timeout" json:"batch_timeout"`

	// WriteTimeout is the timeout for writing messages.
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout"`

	// ReadTimeout is the timeout for reading messages.
	ReadTimeout time.Duration `yaml:"read_timeout" json:"read_timeout"`

	// RequiredAcks is the number of acknowledgments required (0, 1, or -1 for all).
	RequiredAcks int `yaml:"required_acks" json:"required_acks"`

	// MaxMessageBytes is the maximum message size in bytes.
	MaxMessageBytes int `yaml:"max_message_bytes" json:"max_message_bytes"`

	// MinBytes is the minimum number of bytes to fetch.
	MinBytes int `yaml:"min_bytes" json:"min_bytes"`

	// MaxBytes is the maximum number of bytes to fetch.
	MaxBytes int `yaml:"max_bytes" json:"max_bytes"`

	// MaxWait is the maximum time to wait for data.
	MaxWait time.Duration `yaml:"max_wait" json:"max_wait"`
}

// AutoScalingConfig contains auto-scaling configuration for drainers.
type AutoScalingConfig struct {
	// Enabled indicates whether auto-scaling is enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// MinDrainers is the minimum number of drainer workers.
	MinDrainers int `yaml:"min_drainers" json:"min_drainers"`

	// MaxDrainers is the maximum number of drainer workers.
	MaxDrainers int `yaml:"max_drainers" json:"max_drainers"`

	// CPUThresholdHigh is the CPU utilization threshold (0-100) above which
	// the system should scale up drainers.
	CPUThresholdHigh float64 `yaml:"cpu_threshold_high" json:"cpu_threshold_high"`

	// CPUThresholdLow is the CPU utilization threshold (0-100) below which
	// the system should scale down drainers.
	CPUThresholdLow float64 `yaml:"cpu_threshold_low" json:"cpu_threshold_low"`

	// ScaleUpCooldown is the cooldown period after scaling up before
	// another scale-up can occur.
	ScaleUpCooldown time.Duration `yaml:"scale_up_cooldown" json:"scale_up_cooldown"`

	// ScaleDownCooldown is the cooldown period after scaling down before
	// another scale-down can occur.
	ScaleDownCooldown time.Duration `yaml:"scale_down_cooldown" json:"scale_down_cooldown"`

	// MonitoringInterval is the interval at which to check CPU utilization
	// and make scaling decisions.
	MonitoringInterval time.Duration `yaml:"monitoring_interval" json:"monitoring_interval"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		KVStore: KVStoreConfig{
			Type:         "redis",
			Endpoints:    []string{"localhost:6379"},
			ClusterMode:  false,
			MaxRetries:   3,
			PoolSize:     10,
			MinIdleConns: 5,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		},
		Database: DatabaseConfig{
			Type:              "mysql",
			Host:              "localhost",
			Port:              3306,
			MaxOpenConns:      25,
			MaxIdleConns:      5,
			ConnMaxLifetime:   5 * time.Minute,
			ConnMaxIdleTime:   10 * time.Minute,
			ConnectionTimeout: 10 * time.Second,
		},
		Tables: make(map[string]TableConfig),
		WriteBack: WriteBackConfig{
			BatchSize:        100,
			DrainRate:        50, // Default: 50 RPS for DB writes (Redis absorbs high traffic like 200 TPS)
			MaxRetries:       5,
			RetryBackoffBase: 1 * time.Second,
			RetryBackoffMax:  30 * time.Second,
			DefaultTTL:       1 * time.Hour,
			QueueType:        "memory",
			QueueBufferSize:  10000,
			KafkaConfig: KafkaConfig{
				Brokers:         []string{"localhost:9092"},
				Topic:           "shock-absorber-writeback",
				GroupID:         "shock-absorber-writeback",
				BatchSize:       100,
				BatchTimeout:    10 * time.Millisecond,
				WriteTimeout:    10 * time.Second,
				ReadTimeout:     10 * time.Second,
				RequiredAcks:    -1,      // All replicas
				MaxMessageBytes: 1000000, // 1MB
				MinBytes:        1,
				MaxBytes:        10 * 1024 * 1024, // 10MB
				MaxWait:         100 * time.Millisecond,
			},
		},
		AutoScaling: AutoScalingConfig{
			Enabled:            false,
			MinDrainers:        1,
			MaxDrainers:        10,
			CPUThresholdHigh:   80.0,
			CPUThresholdLow:    30.0,
			ScaleUpCooldown:    1 * time.Minute,
			ScaleDownCooldown:  5 * time.Minute,
			MonitoringInterval: 10 * time.Second,
		},
	}
}
