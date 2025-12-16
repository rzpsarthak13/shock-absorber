package core

import (
	"context"
	"time"
)

// KVStore defines the interface for key-value store operations.
// Implementations should support Redis/ElastiCache or similar in-memory stores.
type KVStore interface {
	// Get retrieves a value by key from the store.
	// Returns an error if the key does not exist or if the operation fails.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a key-value pair with an optional TTL.
	// If ttl is 0, the key will not expire.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the store.
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists in the store.
	Exists(ctx context.Context, key string) (bool, error)

	// BatchSet stores multiple key-value pairs atomically with a shared TTL.
	// This is more efficient than multiple Set calls for bulk operations.
	BatchSet(ctx context.Context, items map[string][]byte, ttl time.Duration) error

	// Close closes the connection to the KV store and releases resources.
	Close() error
}

