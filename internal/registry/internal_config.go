package registry

import (
	"time"
)

// InternalConfig represents the internal configuration structure.
// This is a copy of the public Config type to avoid import cycles.
type InternalConfig struct {
	KVStore     InternalKVStoreConfig
	Database    InternalDatabaseConfig
	Tables      map[string]InternalTableConfig
	WriteBack   InternalWriteBackConfig
	AutoScaling InternalAutoScalingConfig
}

// InternalKVStoreConfig contains configuration for the key-value store.
type InternalKVStoreConfig struct {
	Type         string
	Endpoints    []string
	ClusterMode  bool
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// InternalDatabaseConfig contains configuration for the persistent database.
type InternalDatabaseConfig struct {
	Type              string
	Host              string
	Port              int
	Database          string
	Username          string
	Password          string
	SSLMode           string
	MaxOpenConns      int
	MaxIdleConns      int
	ConnMaxLifetime   time.Duration
	ConnMaxIdleTime   time.Duration
	ConnectionTimeout time.Duration
}

// InternalTableConfig contains table-specific configuration overrides.
type InternalTableConfig struct {
	TTL                time.Duration
	WriteBackBatchSize int
	DrainRate          int
	Enabled            bool
	Namespace          string
}

// InternalWriteBackConfig contains global write-back processing configuration.
type InternalWriteBackConfig struct {
	BatchSize        int                 `yaml:"batch_size" json:"batch_size"`
	DrainRate        int                 `yaml:"drain_rate" json:"drain_rate"` // Operations per second for DB writes (e.g., 50 RPS)
	MaxRetries       int                 `yaml:"max_retries" json:"max_retries"`
	RetryBackoffBase time.Duration       `yaml:"retry_backoff_base" json:"retry_backoff_base"`
	RetryBackoffMax  time.Duration       `yaml:"retry_backoff_max" json:"retry_backoff_max"`
	DefaultTTL       time.Duration       `yaml:"default_ttl" json:"default_ttl"`
	QueueType        string              `yaml:"queue_type" json:"queue_type"`
	QueueBufferSize  int                 `yaml:"queue_buffer_size" json:"queue_buffer_size"`
	KafkaConfig      InternalKafkaConfig `yaml:"kafka_config" json:"kafka_config"`
}

// InternalKafkaConfig contains Kafka-specific configuration.
type InternalKafkaConfig struct {
	Brokers         []string      `yaml:"brokers" json:"brokers"`
	Topic           string        `yaml:"topic" json:"topic"`
	GroupID         string        `yaml:"group_id" json:"group_id"`
	BatchSize       int           `yaml:"batch_size" json:"batch_size"`
	BatchTimeout    time.Duration `yaml:"batch_timeout" json:"batch_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	RequiredAcks    int           `yaml:"required_acks" json:"required_acks"`
	MaxMessageBytes int           `yaml:"max_message_bytes" json:"max_message_bytes"`
	MinBytes        int           `yaml:"min_bytes" json:"min_bytes"`
	MaxBytes        int           `yaml:"max_bytes" json:"max_bytes"`
	MaxWait         time.Duration `yaml:"max_wait" json:"max_wait"`
}

// InternalAutoScalingConfig contains auto-scaling configuration for drainers.
type InternalAutoScalingConfig struct {
	Enabled            bool
	MinDrainers        int
	MaxDrainers        int
	CPUThresholdHigh   float64
	CPUThresholdLow    float64
	ScaleUpCooldown    time.Duration
	ScaleDownCooldown  time.Duration
	MonitoringInterval time.Duration
}
