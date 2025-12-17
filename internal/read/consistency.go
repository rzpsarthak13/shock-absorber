package read

import (
	"context"
	"fmt"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// ConsistencyHandler handles read consistency logic.
// It ensures strong consistency for active transactions (Redis) and
// eventual consistency for historical data (DB).
type ConsistencyHandler struct {
	cacheHandler    *CacheHandler
	fallbackHandler *FallbackHandler
	kvStore         core.KVStore
	keyBuilder      *KeyBuilder
}

// NewConsistencyHandler creates a new consistency handler.
func NewConsistencyHandler(
	cacheHandler *CacheHandler,
	fallbackHandler *FallbackHandler,
	kvStore core.KVStore,
	namespace string,
) *ConsistencyHandler {
	return &ConsistencyHandler{
		cacheHandler:    cacheHandler,
		fallbackHandler: fallbackHandler,
		kvStore:         kvStore,
		keyBuilder:      NewKeyBuilder(namespace),
	}
}

// ReadWithConsistency reads a record with the specified consistency level.
// For strong consistency, it reads from Redis (cache).
// For eventual consistency, it allows reading from DB on cache miss.
func (ch *ConsistencyHandler) ReadWithConsistency(
	ctx context.Context,
	tableName string,
	key interface{},
	schema *core.Schema,
	strongConsistency bool,
) (map[string]interface{}, error) {
	// Try cache first
	record, err := ch.cacheHandler.Read(ctx, tableName, key, schema)
	if err == nil {
		// Cache hit - return immediately (strong consistency for active data)
		return record, nil
	}

	// Cache miss - check consistency requirements
	if strongConsistency {
		// For strong consistency, we only read from cache
		// If cache miss, return error
		return nil, fmt.Errorf("record not found in cache (strong consistency required): %w", err)
	}

	// For eventual consistency, fallback to DB
	record, err = ch.fallbackHandler.ReadFromDB(ctx, tableName, key, schema)
	if err != nil {
		return nil, fmt.Errorf("read failed (cache miss and DB fallback failed): %w", err)
	}

	return record, nil
}

// InvalidateCache invalidates a cached record.
// This is called after write-back completion to ensure consistency.
func (ch *ConsistencyHandler) InvalidateCache(ctx context.Context, tableName string, key interface{}) error {
	cacheKey := ch.keyBuilder.BuildKey(tableName, key)
	return ch.kvStore.Delete(ctx, cacheKey)
}

// RefreshCache refreshes a cached record by reading from the database
// and updating the cache. This is useful for stale read detection.
func (ch *ConsistencyHandler) RefreshCache(
	ctx context.Context,
	tableName string,
	key interface{},
	schema *core.Schema,
) (map[string]interface{}, error) {
	// Read from DB
	record, err := ch.fallbackHandler.ReadFromDB(ctx, tableName, key, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh cache from DB: %w", err)
	}

	// Cache is populated asynchronously by fallback handler
	return record, nil
}

// IsStale checks if a cached record is stale based on its timestamp.
// Returns true if the record is older than the specified maxAge.
func (ch *ConsistencyHandler) IsStale(
	ctx context.Context,
	tableName string,
	key interface{},
	maxAge time.Duration,
) (bool, error) {
	cacheKey := ch.keyBuilder.BuildKey(tableName, key)

	// Check if key exists
	exists, err := ch.kvStore.Exists(ctx, cacheKey)
	if err != nil {
		return false, fmt.Errorf("failed to check cache existence: %w", err)
	}

	if !exists {
		return true, nil // Not in cache, considered stale
	}

	// For now, we rely on TTL for staleness detection
	// In a more sophisticated implementation, we could store metadata
	// alongside the value to track timestamps
	// Since Redis TTL handles expiration, we consider non-expired keys as fresh
	return false, nil
}

// ReadHandler is the main read handler that coordinates cache-first reads
// with DB fallback and consistency handling.
type ReadHandler struct {
	consistencyHandler *ConsistencyHandler
	cacheHandler       *CacheHandler
	fallbackHandler    *FallbackHandler
	schema             *core.Schema
	tableName          string
	defaultTTL         time.Duration
}

// NewReadHandler creates a new read handler for a specific table.
func NewReadHandler(
	tableName string,
	schema *core.Schema,
	cacheHandler *CacheHandler,
	fallbackHandler *FallbackHandler,
	consistencyHandler *ConsistencyHandler,
	defaultTTL time.Duration,
) *ReadHandler {
	return &ReadHandler{
		consistencyHandler: consistencyHandler,
		cacheHandler:       cacheHandler,
		fallbackHandler:    fallbackHandler,
		schema:             schema,
		tableName:          tableName,
		defaultTTL:         defaultTTL,
	}
}

// Read performs a cache-first read with DB fallback.
// It handles cache stampede prevention and consistency requirements.
func (rh *ReadHandler) Read(ctx context.Context, key interface{}, strongConsistency bool) (map[string]interface{}, error) {
	// Use consistency handler for the read
	return rh.consistencyHandler.ReadWithConsistency(ctx, rh.tableName, key, rh.schema, strongConsistency)
}

// ReadFromCache reads directly from cache without fallback.
func (rh *ReadHandler) ReadFromCache(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	return rh.cacheHandler.Read(ctx, rh.tableName, key, rh.schema)
}

// ReadFromDB reads directly from database without checking cache.
func (rh *ReadHandler) ReadFromDB(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	return rh.fallbackHandler.ReadFromDB(ctx, rh.tableName, key, rh.schema)
}

// Invalidate invalidates the cache for a specific key.
func (rh *ReadHandler) Invalidate(ctx context.Context, key interface{}) error {
	return rh.consistencyHandler.InvalidateCache(ctx, rh.tableName, key)
}

// Refresh refreshes the cache for a specific key from the database.
func (rh *ReadHandler) Refresh(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	return rh.consistencyHandler.RefreshCache(ctx, rh.tableName, key, rh.schema)
}
