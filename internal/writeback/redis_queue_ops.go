package writeback

import (
	"context"
)

// RedisQueueOperations extends KVStore with Redis-specific list operations
// needed for efficient queue operations. This interface should be implemented
// by Redis-specific KV store implementations.
type RedisQueueOperations interface {
	// ListPush adds a value to the end of a list (RPUSH).
	ListPush(ctx context.Context, key string, value []byte) error

	// ListPop removes and returns the first element from a list (LPOP).
	// Returns nil if the list is empty.
	ListPop(ctx context.Context, key string) ([]byte, error)

	// ListLength returns the length of a list (LLEN).
	ListLength(ctx context.Context, key string) (int64, error)

	// ListRange returns a range of elements from a list (LRANGE).
	ListRange(ctx context.Context, key string, start, stop int64) ([][]byte, error)

	// ListTrim trims a list to the specified range (LTRIM).
	ListTrim(ctx context.Context, key string, start, stop int64) error
}
