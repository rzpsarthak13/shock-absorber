package kvstore

import (
	"fmt"
	"sync"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// KVStoreFactory is the Strategy interface for creating KV store implementations.
// Each backend (Redis, DynamoDB, etc.) implements this interface to provide
// its own factory method.
type KVStoreFactory interface {
	// Create creates a new KV store instance based on the provided configuration.
	Create(config KVStoreConfig) (core.KVStore, error)

	// Type returns the type identifier for this factory (e.g., "redis", "dynamodb").
	Type() string

	// Validate validates the configuration specific to this KV store type.
	Validate(config KVStoreConfig) error
}

// KVStoreConfig represents the configuration needed to create a KV store.
// Supports multiple NoSQL backends (Redis, DynamoDB, etc.) through a plugin-based architecture.
type KVStoreConfig struct {
	Type         string
	Endpoints    []string
	ClusterMode  bool
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	MinIdleConns int
	DialTimeout  int64 // nanoseconds
	ReadTimeout  int64 // nanoseconds
	WriteTimeout int64 // nanoseconds
	
	// DynamoDB-specific fields
	Region         string
	TableName      string
	Endpoint       string // Optional, for LocalStack
	AccessKeyID    string // Optional, can use IAM role instead
	SecretAccessKey string // Optional, can use IAM role instead
}

var (
	// factoryRegistry stores all registered KV store factories.
	factoryRegistry = make(map[string]KVStoreFactory)

	// registryMutex protects the registries from concurrent access.
	registryMutex sync.RWMutex
)

// RegisterFactory registers a KV store factory.
// This is called automatically by each implementation's init() function.
func RegisterFactory(factory KVStoreFactory) {
	if factory == nil {
		panic("factory cannot be nil")
	}
	if factory.Type() == "" {
		panic("factory type cannot be empty")
	}

	registryMutex.Lock()
	defer registryMutex.Unlock()

	if _, exists := factoryRegistry[factory.Type()]; exists {
		panic(fmt.Sprintf("factory for type %q is already registered", factory.Type()))
	}

	factoryRegistry[factory.Type()] = factory
}


// Create creates a KV store instance using the appropriate factory based on config.Type.
// Uses Strategy pattern - no if-else statements needed.
func Create(config KVStoreConfig) (core.KVStore, error) {
	if config.Type == "" {
		return nil, fmt.Errorf("kvstore type is required")
	}

	registryMutex.RLock()
	factory, exists := factoryRegistry[config.Type]
	registryMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported KV store type: %s", config.Type)
	}

	// Validate config using the factory's validation strategy
	if err := factory.Validate(config); err != nil {
		return nil, fmt.Errorf("invalid configuration for %s: %w", config.Type, err)
	}

	// Use strategy pattern - factory.Create handles the specific implementation
	return factory.Create(config)
}


// GetRegisteredTypes returns a list of all registered KV store types.
func GetRegisteredTypes() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	types := make([]string, 0, len(factoryRegistry))
	for t := range factoryRegistry {
		types = append(types, t)
	}
	return types
}

// IsTypeRegistered checks if a KV store type is registered.
func IsTypeRegistered(storeType string) bool {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	_, exists := factoryRegistry[storeType]
	return exists
}

