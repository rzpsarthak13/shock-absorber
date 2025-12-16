package core

import (
	"context"
)

// Table defines the interface for table operations.
// This provides a high-level abstraction for CRUD operations that
// automatically handles KV store caching and write-back to the database.
type Table interface {
	// Create inserts a new record into the table.
	// The record is first written to the KV store (write-ahead log),
	// then asynchronously written back to the database.
	Create(ctx context.Context, record map[string]interface{}) error

	// Read retrieves a record by its primary key.
	// First checks the KV store (cache), then falls back to the database
	// if not found in cache.
	Read(ctx context.Context, key interface{}) (map[string]interface{}, error)

	// Update modifies an existing record identified by the primary key.
	// Updates are written to the KV store first, then asynchronously
	// written back to the database.
	Update(ctx context.Context, key interface{}, updates map[string]interface{}) error

	// Delete removes a record by its primary key.
	// Deletions are written to the KV store first, then asynchronously
	// written back to the database.
	Delete(ctx context.Context, key interface{}) error

	// EnableKV enables KV store caching for this table.
	// This must be called before using the table for KV operations.
	EnableKV() error

	// DisableKV disables KV store caching for this table.
	// After disabling, operations will go directly to the database.
	DisableKV() error

	// FindByField queries the database for a record by a non-primary key field.
	// This is useful for querying by fields like reference_id.
	// It first queries the database to find the primary key, then reads the record
	// (which will check Redis cache first if KV is enabled).
	FindByField(ctx context.Context, fieldName string, fieldValue interface{}) (map[string]interface{}, error)

	// ExecuteWriteOperation executes a write operation directly to the database.
	// This is used by the write-back drainer to process queued operations.
	// It bypasses the KV store and writes directly to the persistent database.
	ExecuteWriteOperation(ctx context.Context, operation *WriteOperation) error

	// GetWriteBackQueue returns the write-back queue for this table.
	// This is used by the drainer to dequeue operations.
	GetWriteBackQueue() WriteBackQueue
}
