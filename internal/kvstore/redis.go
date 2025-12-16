package kvstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisKVStore implements the core.KVStore interface using Redis.
type RedisKVStore struct {
	client *redis.Client
	closed bool
}

// NewRedisKVStore creates a new Redis KV store implementation.
func NewRedisKVStore(endpoints []string, password string, db int, poolSize int, minIdleConns int, dialTimeout, readTimeout, writeTimeout time.Duration) (*RedisKVStore, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("at least one endpoint is required")
	}

	// For now, we only support single-node Redis (not cluster)
	// TODO: Add cluster support
	opts := &redis.Options{
		Addr:         endpoints[0],
		Password:     password,
		DB:           db,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisKVStore{
		client: client,
		closed: false,
	}, nil
}

// Get retrieves a value by key from the store.
func (r *RedisKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if r.closed {
		return nil, fmt.Errorf("KV store is closed")
	}

	log.Printf("[REDIS] GET operation - Key: %s", key)
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		log.Printf("[REDIS] Key not found: %s", key)
		return nil, fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		log.Printf("[REDIS] ERROR: Failed to get key %s: %v", key, err)
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	log.Printf("[REDIS] Successfully retrieved key %s (value size: %d bytes)", key, len(val))
	// Log a preview of the value
	var valuePreview map[string]interface{}
	if err := json.Unmarshal(val, &valuePreview); err == nil {
		log.Printf("[REDIS] Value Preview: %+v", valuePreview)
	}
	return val, nil
}

// Set stores a key-value pair with an optional TTL.
func (r *RedisKVStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if r.closed {
		return fmt.Errorf("KV store is closed")
	}

	log.Printf("[REDIS] SET operation - Key: %s, Value Size: %d bytes, TTL: %v", key, len(value), ttl)

	// Log a preview of the value (first 200 chars)
	valuePreview := string(value)
	if len(valuePreview) > 200 {
		valuePreview = valuePreview[:200] + "..."
	}
	log.Printf("[REDIS] Value Preview: %s", valuePreview)

	// Also log as JSON if possible
	var jsonPreview map[string]interface{}
	if err := json.Unmarshal(value, &jsonPreview); err == nil {
		log.Printf("[REDIS] Value as JSON: %+v", jsonPreview)
	}

	if ttl > 0 {
		err := r.client.Set(ctx, key, value, ttl).Err()
		if err != nil {
			log.Printf("[REDIS] ERROR: Failed to set key %s: %v", key, err)
			return fmt.Errorf("failed to set key %s: %w", key, err)
		}
		log.Printf("[REDIS] Successfully stored key %s in Redis with TTL %v", key, ttl)
	} else {
		err := r.client.Set(ctx, key, value, 0).Err()
		if err != nil {
			log.Printf("[REDIS] ERROR: Failed to set key %s: %v", key, err)
			return fmt.Errorf("failed to set key %s: %w", key, err)
		}
		log.Printf("[REDIS] Successfully stored key %s in Redis (no expiration)", key)
	}

	return nil
}

// Delete removes a key from the store.
func (r *RedisKVStore) Delete(ctx context.Context, key string) error {
	if r.closed {
		return fmt.Errorf("KV store is closed")
	}

	err := r.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

// Exists checks if a key exists in the store.
func (r *RedisKVStore) Exists(ctx context.Context, key string) (bool, error) {
	if r.closed {
		return false, fmt.Errorf("KV store is closed")
	}

	count, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check existence of key %s: %w", key, err)
	}

	return count > 0, nil
}

// BatchSet stores multiple key-value pairs atomically with a shared TTL.
func (r *RedisKVStore) BatchSet(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if r.closed {
		return fmt.Errorf("KV store is closed")
	}

	pipe := r.client.Pipeline()
	for key, value := range items {
		if ttl > 0 {
			pipe.Set(ctx, key, value, ttl)
		} else {
			pipe.Set(ctx, key, value, 0)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to batch set keys: %w", err)
	}

	return nil
}

// Close closes the connection to the KV store.
func (r *RedisKVStore) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true
	return r.client.Close()
}

// GetClient returns the underlying Redis client for advanced operations.
func (r *RedisKVStore) GetClient() *redis.Client {
	return r.client
}

// ListPush adds a value to the end of a list (RPUSH).
func (r *RedisKVStore) ListPush(ctx context.Context, key string, value []byte) error {
	if r.closed {
		return fmt.Errorf("KV store is closed")
	}
	return r.client.RPush(ctx, key, value).Err()
}

// ListPop removes and returns the first element from a list (LPOP).
func (r *RedisKVStore) ListPop(ctx context.Context, key string) ([]byte, error) {
	if r.closed {
		return nil, fmt.Errorf("KV store is closed")
	}
	val, err := r.client.LPop(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil // List is empty
	}
	return val, err
}

// ListLength returns the length of a list (LLEN).
func (r *RedisKVStore) ListLength(ctx context.Context, key string) (int64, error) {
	if r.closed {
		return 0, fmt.Errorf("KV store is closed")
	}
	return r.client.LLen(ctx, key).Result()
}

// ListRange returns a range of elements from a list (LRANGE).
func (r *RedisKVStore) ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	if r.closed {
		return nil, fmt.Errorf("KV store is closed")
	}
	vals, err := r.client.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, err
	}
	result := make([][]byte, len(vals))
	for i, v := range vals {
		result[i] = []byte(v)
	}
	return result, nil
}

// ListTrim trims a list to the specified range (LTRIM).
func (r *RedisKVStore) ListTrim(ctx context.Context, key string, start, stop int64) error {
	if r.closed {
		return fmt.Errorf("KV store is closed")
	}
	return r.client.LTrim(ctx, key, start, stop).Err()
}
