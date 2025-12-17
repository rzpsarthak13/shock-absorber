package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigValidator is the Strategy interface for validating configuration.
// Each backend (Redis, DynamoDB, etc.) provides its own validator to validate
// backend-specific configuration using the Strategy pattern.
type ConfigValidator interface {
	// Validate validates the internal configuration for this KV store type.
	// It should validate only the KVStore-specific configuration.
	Validate(config *InternalConfig) error

	// Type returns the type identifier for this validator (e.g., "redis", "dynamodb").
	Type() string
}

var (
	// validatorRegistry stores all registered config validators.
	validatorRegistry = make(map[string]ConfigValidator)

	// validatorRegistryMutex protects the validator registry from concurrent access.
	validatorRegistryMutex sync.RWMutex
)

// ValidationStrategyRegistry provides methods to register and retrieve config validators.
// This implements the Strategy pattern for configuration validation.
type ValidationStrategyRegistry struct{}

// RegisterValidator registers a config validator.
// This is called automatically by each implementation's init() function.
// Panics if validator is nil, type is empty, or type is already registered.
func (r *ValidationStrategyRegistry) Register(validator ConfigValidator) {
	if validator == nil {
		panic("validator cannot be nil")
	}
	if validator.Type() == "" {
		panic("validator type cannot be empty")
	}

	validatorRegistryMutex.Lock()
	defer validatorRegistryMutex.Unlock()

	if _, exists := validatorRegistry[validator.Type()]; exists {
		panic(fmt.Sprintf("validator for type %q is already registered", validator.Type()))
	}

	validatorRegistry[validator.Type()] = validator
}

// Get retrieves a validator by type.
// Returns the validator and true if found, nil and false otherwise.
func (r *ValidationStrategyRegistry) Get(validatorType string) (ConfigValidator, bool) {
	validatorRegistryMutex.RLock()
	defer validatorRegistryMutex.RUnlock()

	validator, exists := validatorRegistry[validatorType]
	return validator, exists
}

// RegisterValidator is a convenience function to register a validator using the default registry.
// This is the preferred way to register validators from init() functions.
func RegisterValidator(validator ConfigValidator) {
	defaultValidationRegistry.Register(validator)
}

// GetValidator is a convenience function to retrieve a validator by type using the default registry.
func GetValidator(validatorType string) (ConfigValidator, bool) {
	return defaultValidationRegistry.Get(validatorType)
}

// defaultValidationRegistry is the default instance of ValidationStrategyRegistry.
var defaultValidationRegistry = &ValidationStrategyRegistry{}

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
			Type: "redis",
			RedisConfig: InternalRedisConfig{
				Endpoints:    []string{"localhost:6379"},
				ClusterMode:  false,
				DB:           0,
				PoolSize:     10,
				MinIdleConns: 5,
			},
			MaxRetries:   3,
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
		config.KVStore.RedisConfig.Endpoints = strings.Split(val, ",")
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_CLUSTER_MODE"); val != "" {
		config.KVStore.RedisConfig.ClusterMode = (val == "true" || val == "1")
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_PASSWORD"); val != "" {
		config.KVStore.RedisConfig.Password = val
	}
	if val := os.Getenv("SHOCK_ABSORBER_KVSTORE_DB"); val != "" {
		var db int
		if _, err := fmt.Sscanf(val, "%d", &db); err == nil {
			config.KVStore.RedisConfig.DB = db
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
			config.KVStore.RedisConfig.PoolSize = size
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
// Uses Strategy pattern for KVStore validation - no if-else statements needed.
func (cm *ConfigManager) validateConfig(config *InternalConfig) error {
	// Validate KV Store configuration using Strategy pattern
	if config.KVStore.Type == "" {
		return fmt.Errorf("kvstore.type is required")
	}

	// Get validator from registry based on config type (Strategy pattern)
	validator, exists := GetValidator(config.KVStore.Type)
	if !exists {
		return fmt.Errorf("unsupported KV store type: %s", config.KVStore.Type)
	}

	// Use strategy - validator.Validate handles the specific validation
	if err := validator.Validate(config); err != nil {
		return fmt.Errorf("kvstore validation failed: %w", err)
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
