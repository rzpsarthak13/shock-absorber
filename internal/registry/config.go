package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigManager handles loading and managing configuration from various sources.
type ConfigManager struct {
	config *InternalConfig
}

// NewConfigManager creates a new configuration manager with default configuration.
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		config: defaultInternalConfig(),
	}
}

// defaultInternalConfig returns a configuration with sensible defaults.
func defaultInternalConfig() *InternalConfig {
	return &InternalConfig{
		KVStore: InternalKVStoreConfig{
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
		Database: InternalDatabaseConfig{
			Type:              "mysql",
			Host:              "localhost",
			Port:              3306,
			MaxOpenConns:      25,
			MaxIdleConns:      5,
			ConnMaxLifetime:   5 * time.Minute,
			ConnMaxIdleTime:   10 * time.Minute,
			ConnectionTimeout: 10 * time.Second,
		},
		Tables: make(map[string]InternalTableConfig),
		WriteBack: InternalWriteBackConfig{
			BatchSize:        100,
			DrainRate:        50, // Default: 50 RPS for DB writes (Redis absorbs high traffic)
			MaxRetries:       5,
			RetryBackoffBase: 1 * time.Second,
			RetryBackoffMax:  30 * time.Second,
			DefaultTTL:       1 * time.Hour,
			QueueType:        "memory",
			QueueBufferSize:  10000,
			KafkaConfig: InternalKafkaConfig{
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
		AutoScaling: InternalAutoScalingConfig{
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

// LoadFromFile loads configuration from a YAML or JSON file.
// The file format is determined by the file extension (.yaml, .yml, or .json).
func (cm *ConfigManager) LoadFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return cm.LoadFromYAML(data)
	case ".json":
		return cm.LoadFromJSON(data)
	default:
		return fmt.Errorf("unsupported config file format: %s (supported: .yaml, .yml, .json)", ext)
	}
}

// LoadFromYAML loads configuration from YAML data.
func (cm *ConfigManager) LoadFromYAML(data []byte) error {
	config := defaultInternalConfig()
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, config); err != nil {
			return fmt.Errorf("failed to parse YAML config: %w", err)
		}
	}

	if err := cm.validateConfig(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cm.config = config
	return nil
}

// LoadFromJSON loads configuration from JSON data.
func (cm *ConfigManager) LoadFromJSON(data []byte) error {
	config := defaultInternalConfig()
	if len(data) > 0 {
		if err := json.Unmarshal(data, config); err != nil {
			return fmt.Errorf("failed to parse JSON config: %w", err)
		}
	}

	if err := cm.validateConfig(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cm.config = config
	return nil
}

// LoadFromEnv loads configuration from environment variables.
// Environment variables follow the pattern: SHOCK_ABSORBER_<SECTION>_<KEY>
// Examples:
//   - SHOCK_ABSORBER_KVSTORE_TYPE=redis
//   - SHOCK_ABSORBER_KVSTORE_ENDPOINTS=localhost:6379,localhost:6380
//   - SHOCK_ABSORBER_DATABASE_HOST=localhost
//   - SHOCK_ABSORBER_DATABASE_PORT=3306
//   - SHOCK_ABSORBER_WRITEBACK_BATCH_SIZE=100
func (cm *ConfigManager) LoadFromEnv() error {
	config := defaultInternalConfig()

	// KV Store configuration
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_TYPE"); val != "" {
		config.KVStore.Type = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_ENDPOINTS"); val != "" {
		config.KVStore.Endpoints = strings.Split(val, ",")
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_CLUSTER_MODE"); val != "" {
		config.KVStore.ClusterMode = (val == "true" || val == "1")
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_PASSWORD"); val != "" {
		config.KVStore.Password = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_DB"); val != "" {
		var db int
		if _, err := fmt.Sscanf(val, "%d", &db); err == nil {
			config.KVStore.DB = db
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_MAX_RETRIES"); val != "" {
		var retries int
		if _, err := fmt.Sscanf(val, "%d", &retries); err == nil {
			config.KVStore.MaxRetries = retries
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_POOL_SIZE"); val != "" {
		var size int
		if _, err := fmt.Sscanf(val, "%d", &size); err == nil {
			config.KVStore.PoolSize = size
		}
	}

	// Database configuration
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_TYPE"); val != "" {
		config.Database.Type = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_HOST"); val != "" {
		config.Database.Host = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_PORT"); val != "" {
		var port int
		if _, err := fmt.Sscanf(val, "%d", &port); err == nil {
			config.Database.Port = port
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_DATABASE"); val != "" {
		config.Database.Database = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_USERNAME"); val != "" {
		config.Database.Username = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_PASSWORD"); val != "" {
		config.Database.Password = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_SSL_MODE"); val != "" {
		config.Database.SSLMode = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_MAX_OPEN_CONNS"); val != "" {
		var maxOpen int
		if _, err := fmt.Sscanf(val, "%d", &maxOpen); err == nil {
			config.Database.MaxOpenConns = maxOpen
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_DATABASE_MAX_IDLE_CONNS"); val != "" {
		var maxIdle int
		if _, err := fmt.Sscanf(val, "%d", &maxIdle); err == nil {
			config.Database.MaxIdleConns = maxIdle
		}
	}

	// Write-back configuration
	if val := os.Getenv("SHOCK_ABSORBER_WRITEBACK_BATCH_SIZE"); val != "" {
		var batchSize int
		if _, err := fmt.Sscanf(val, "%d", &batchSize); err == nil {
			config.WriteBack.BatchSize = batchSize
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_WRITEBACK_DRAIN_RATE"); val != "" {
		var drainRate int
		if _, err := fmt.Sscanf(val, "%d", &drainRate); err == nil {
			config.WriteBack.DrainRate = drainRate
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_WRITEBACK_MAX_RETRIES"); val != "" {
		var maxRetries int
		if _, err := fmt.Sscanf(val, "%d", &maxRetries); err == nil {
			config.WriteBack.MaxRetries = maxRetries
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_WRITEBACK_DEFAULT_TTL"); val != "" {
		if ttl, err := time.ParseDuration(val); err == nil {
			config.WriteBack.DefaultTTL = ttl
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_WRITEBACK_QUEUE_TYPE"); val != "" {
		config.WriteBack.QueueType = val
	}

	// Auto-scaling configuration
	if val := os.Getenv("SHOCK_ABSORBER_AUTOSCALING_ENABLED"); val != "" {
		config.AutoScaling.Enabled = (val == "true" || val == "1")
	}
	if val := os.Getenv("SHOCK_ABSORBER_AUTOSCALING_MIN_DRAINERS"); val != "" {
		var minDrainers int
		if _, err := fmt.Sscanf(val, "%d", &minDrainers); err == nil {
			config.AutoScaling.MinDrainers = minDrainers
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_AUTOSCALING_MAX_DRAINERS"); val != "" {
		var maxDrainers int
		if _, err := fmt.Sscanf(val, "%d", &maxDrainers); err == nil {
			config.AutoScaling.MaxDrainers = maxDrainers
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_AUTOSCALING_CPU_THRESHOLD_HIGH"); val != "" {
		var threshold float64
		if _, err := fmt.Sscanf(val, "%f", &threshold); err == nil {
			config.AutoScaling.CPUThresholdHigh = threshold
		}
	}
	if val := os.Getenv("SHOCK_ABSORBER_AUTOSCALING_CPU_THRESHOLD_LOW"); val != "" {
		var threshold float64
		if _, err := fmt.Sscanf(val, "%f", &threshold); err == nil {
			config.AutoScaling.CPUThresholdLow = threshold
		}
	}

	if err := cm.validateConfig(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cm.config = config
	return nil
}

// GetConfig returns the current internal configuration.
func (cm *ConfigManager) GetConfig() *InternalConfig {
	return cm.config
}

// GetTableConfig returns the configuration for a specific table.
// If table-specific configuration exists, it is merged with defaults.
func (cm *ConfigManager) GetTableConfig(tableName string) InternalTableConfig {
	tableConfig, exists := cm.config.Tables[tableName]
	if !exists {
		// Return default table config
		return InternalTableConfig{
			TTL:                cm.config.WriteBack.DefaultTTL,
			WriteBackBatchSize: cm.config.WriteBack.BatchSize,
			DrainRate:          cm.config.WriteBack.DrainRate,
			Enabled:            false,
		}
	}

	// Merge with defaults
	if tableConfig.TTL == 0 {
		tableConfig.TTL = cm.config.WriteBack.DefaultTTL
	}
	if tableConfig.WriteBackBatchSize == 0 {
		tableConfig.WriteBackBatchSize = cm.config.WriteBack.BatchSize
	}
	if tableConfig.DrainRate == 0 {
		tableConfig.DrainRate = cm.config.WriteBack.DrainRate
	}

	return tableConfig
}

// validateConfig validates the configuration and returns an error if invalid.
func (cm *ConfigManager) validateConfig(config *InternalConfig) error {
	// Validate KV Store configuration
	if config.KVStore.Type == "" {
		return fmt.Errorf("kvstore.type is required")
	}
	if config.KVStore.Type != "redis" && config.KVStore.Type != "elasticache" {
		return fmt.Errorf("kvstore.type must be 'redis' or 'elasticache'")
	}
	if len(config.KVStore.Endpoints) == 0 {
		return fmt.Errorf("kvstore.endpoints must contain at least one endpoint")
	}
	if config.KVStore.PoolSize <= 0 {
		return fmt.Errorf("kvstore.pool_size must be greater than 0")
	}

	// Validate Database configuration
	if config.Database.Type == "" {
		return fmt.Errorf("database.type is required")
	}
	if config.Database.Type != "mysql" && config.Database.Type != "postgresql" {
		return fmt.Errorf("database.type must be 'mysql' or 'postgresql'")
	}
	if config.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if config.Database.Port <= 0 || config.Database.Port > 65535 {
		return fmt.Errorf("database.port must be between 1 and 65535")
	}
	if config.Database.Database == "" {
		return fmt.Errorf("database.database is required")
	}
	if config.Database.Username == "" {
		return fmt.Errorf("database.username is required")
	}
	if config.Database.MaxOpenConns <= 0 {
		return fmt.Errorf("database.max_open_conns must be greater than 0")
	}

	// Validate Write-back configuration
	if config.WriteBack.BatchSize <= 0 {
		return fmt.Errorf("writeback.batch_size must be greater than 0")
	}
	if config.WriteBack.DrainRate <= 0 {
		return fmt.Errorf("writeback.drain_rate must be greater than 0")
	}
	if config.WriteBack.MaxRetries < 0 {
		return fmt.Errorf("writeback.max_retries must be non-negative")
	}
	if config.WriteBack.DefaultTTL <= 0 {
		return fmt.Errorf("writeback.default_ttl must be greater than 0")
	}
	if config.WriteBack.QueueType != "" && config.WriteBack.QueueType != "memory" && config.WriteBack.QueueType != "redis" && config.WriteBack.QueueType != "kafka" {
		return fmt.Errorf("writeback.queue_type must be 'memory', 'redis', or 'kafka'")
	}

	// Validate Kafka config if queue type is kafka
	if config.WriteBack.QueueType == "kafka" {
		if len(config.WriteBack.KafkaConfig.Brokers) == 0 {
			return fmt.Errorf("kafka_config.brokers is required when queue_type is 'kafka'")
		}
		if config.WriteBack.KafkaConfig.Topic == "" {
			return fmt.Errorf("kafka_config.topic is required when queue_type is 'kafka'")
		}
	}

	// Validate Auto-scaling configuration
	if config.AutoScaling.Enabled {
		if config.AutoScaling.MinDrainers <= 0 {
			return fmt.Errorf("autoscaling.min_drainers must be greater than 0")
		}
		if config.AutoScaling.MaxDrainers < config.AutoScaling.MinDrainers {
			return fmt.Errorf("autoscaling.max_drainers must be >= autoscaling.min_drainers")
		}
		if config.AutoScaling.CPUThresholdHigh <= 0 || config.AutoScaling.CPUThresholdHigh > 100 {
			return fmt.Errorf("autoscaling.cpu_threshold_high must be between 0 and 100")
		}
		if config.AutoScaling.CPUThresholdLow <= 0 || config.AutoScaling.CPUThresholdLow > 100 {
			return fmt.Errorf("autoscaling.cpu_threshold_low must be between 0 and 100")
		}
		if config.AutoScaling.CPUThresholdLow >= config.AutoScaling.CPUThresholdHigh {
			return fmt.Errorf("autoscaling.cpu_threshold_low must be < autoscaling.cpu_threshold_high")
		}
		if config.AutoScaling.MonitoringInterval <= 0 {
			return fmt.Errorf("autoscaling.monitoring_interval must be greater than 0")
		}
	}

	return nil
}
