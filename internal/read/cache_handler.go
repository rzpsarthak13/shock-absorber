package read

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/razorpay/shock-absorber/internal/core"
)

// CacheHandler handles cache-first read operations.
// It checks the KV store first and handles cache expiration gracefully.
type CacheHandler struct {
	kvStore           core.KVStore
	translator        core.SchemaTranslator
	keyBuilder        *KeyBuilder
	stampedePreventer *StampedePreventer
}

// NewCacheHandler creates a new cache handler.
func NewCacheHandler(kvStore core.KVStore, translator core.SchemaTranslator, namespace string) *CacheHandler {
	return &CacheHandler{
		kvStore:           kvStore,
		translator:        translator,
		keyBuilder:        NewKeyBuilder(namespace),
		stampedePreventer: NewStampedePreventer(),
	}
}

// Read attempts to read a record from the cache by primary key.
// Returns the record if found, or an error if not found or if the operation fails.
func (ch *CacheHandler) Read(ctx context.Context, tableName string, key interface{}, schema *core.Schema) (map[string]interface{}, error) {
	// Build the cache key
	cacheKey := ch.keyBuilder.BuildKey(tableName, key)

	// Check if there's an ongoing read for this key (stampede prevention)
	if ch.stampedePreventer.IsInFlight(cacheKey) {
		// Wait for the in-flight request to complete
		return ch.stampedePreventer.WaitForResult(ctx, cacheKey)
	}

	// Mark this key as in-flight
	ch.stampedePreventer.MarkInFlight(cacheKey)
	defer ch.stampedePreventer.MarkComplete(cacheKey)

	// Try to get from cache
	value, err := ch.kvStore.Get(ctx, cacheKey)
	if err != nil {
		// Cache miss or error - signal to fallback handler
		ch.stampedePreventer.SetError(cacheKey, fmt.Errorf("cache miss: %w", err))
		return nil, fmt.Errorf("cache miss: %w", err)
	}

	// Deserialize the value
	record, err := ch.translator.FromKV(cacheKey, value, schema)
	if err != nil {
		ch.stampedePreventer.SetError(cacheKey, fmt.Errorf("failed to deserialize cache value: %w", err))
		return nil, fmt.Errorf("failed to deserialize cache value: %w", err)
	}

	// Success - set result for any waiting goroutines
	ch.stampedePreventer.SetResult(cacheKey, record)
	return record, nil
}

// Exists checks if a key exists in the cache.
func (ch *CacheHandler) Exists(ctx context.Context, tableName string, key interface{}) (bool, error) {
	cacheKey := ch.keyBuilder.BuildKey(tableName, key)
	return ch.kvStore.Exists(ctx, cacheKey)
}

// KeyBuilder builds cache keys in the format: {namespace}:{table}:{primary_key_value}
type KeyBuilder struct {
	namespace string
}

// NewKeyBuilder creates a new key builder.
func NewKeyBuilder(namespace string) *KeyBuilder {
	return &KeyBuilder{namespace: namespace}
}

// BuildKey constructs a cache key for the given table and primary key value.
func (kb *KeyBuilder) BuildKey(tableName string, key interface{}) string {
	keyStr := fmt.Sprintf("%v", key)
	if kb.namespace != "" {
		return fmt.Sprintf("%s:%s:%s", kb.namespace, tableName, keyStr)
	}
	return fmt.Sprintf("%s:%s", tableName, keyStr)
}

// StampedePreventer prevents cache stampede by coordinating concurrent reads
// for the same key. Only one request will actually hit the cache/DB, while
// others wait for the result.
type StampedePreventer struct {
	mu      sync.RWMutex
	pending map[string]*pendingRead
}

type pendingRead struct {
	mu       sync.Mutex
	result   map[string]interface{}
	err      error
	done     chan struct{}
	waiters  int
	deadline time.Time
}

// NewStampedePreventer creates a new stampede preventer.
func NewStampedePreventer() *StampedePreventer {
	return &StampedePreventer{
		pending: make(map[string]*pendingRead),
	}
}

// IsInFlight checks if there's already an in-flight read for the given key.
func (sp *StampedePreventer) IsInFlight(key string) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	p, exists := sp.pending[key]
	if !exists {
		return false
	}

	// Check if expired
	if time.Now().After(p.deadline) {
		return false
	}

	return true
}

// MarkInFlight marks a key as having an in-flight read operation.
func (sp *StampedePreventer) MarkInFlight(key string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Create or update pending read
	p, exists := sp.pending[key]
	if !exists {
		p = &pendingRead{
			done:     make(chan struct{}),
			deadline: time.Now().Add(30 * time.Second), // 30s timeout for in-flight operations
		}
		sp.pending[key] = p
	}
	p.waiters++
}

// MarkComplete marks a key as no longer having an in-flight read operation.
func (sp *StampedePreventer) MarkComplete(key string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	p, exists := sp.pending[key]
	if !exists {
		return
	}

	p.waiters--
	if p.waiters <= 0 {
		// Clean up after a short delay to allow any last waiters to see the result
		go func() {
			time.Sleep(100 * time.Millisecond)
			sp.mu.Lock()
			defer sp.mu.Unlock()
			if p, exists := sp.pending[key]; exists && p.waiters <= 0 {
				delete(sp.pending, key)
			}
		}()
	}
}

// SetResult sets the result for a pending read and notifies all waiters.
func (sp *StampedePreventer) SetResult(key string, result map[string]interface{}) {
	sp.mu.RLock()
	p, exists := sp.pending[key]
	sp.mu.RUnlock()

	if !exists {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.result == nil && p.err == nil {
		p.result = result
		close(p.done)
	}
}

// SetError sets an error for a pending read and notifies all waiters.
func (sp *StampedePreventer) SetError(key string, err error) {
	sp.mu.RLock()
	p, exists := sp.pending[key]
	sp.mu.RUnlock()

	if !exists {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.result == nil && p.err == nil {
		p.err = err
		close(p.done)
	}
}

// WaitForResult waits for an in-flight read to complete and returns its result.
func (sp *StampedePreventer) WaitForResult(ctx context.Context, key string) (map[string]interface{}, error) {
	sp.mu.RLock()
	p, exists := sp.pending[key]
	sp.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no pending read for key: %s", key)
	}

	// Wait for the result with context cancellation support
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.err != nil {
			return nil, p.err
		}
		// Return a copy to prevent external modifications
		result := make(map[string]interface{})
		for k, v := range p.result {
			result[k] = v
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CacheMetadata represents metadata stored alongside cached values.
// This is used for stale read detection and cache invalidation.
type CacheMetadata struct {
	Timestamp time.Time `json:"timestamp"`
	Version   int64     `json:"version,omitempty"`
	Source    string    `json:"source"` // "cache" or "db"
}

// EncodeMetadata encodes cache metadata into JSON.
func EncodeMetadata(metadata *CacheMetadata) ([]byte, error) {
	return json.Marshal(metadata)
}

// DecodeMetadata decodes cache metadata from JSON.
func DecodeMetadata(data []byte) (*CacheMetadata, error) {
	var metadata CacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}
