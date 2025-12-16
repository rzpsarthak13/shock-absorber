package shockabsorber

import (
	"context"

	"github.com/razorpay/shock-absorber/internal/core"
)

// Table provides a high-level interface for performing CRUD operations on a table.
// It automatically handles KV store caching and write-back to the database.
// This is the public-facing interface that wraps the internal core.Table interface.
type Table interface {
	// Create inserts a new record into the table.
	// The record is first written to the KV store (write-ahead log),
	// then asynchronously written back to the database.
	// The record should be a map[string]interface{} where keys are column names
	// and values are the column values.
	Create(ctx context.Context, record map[string]interface{}) error

	// Read retrieves a record by its primary key.
	// First checks the KV store (cache), then falls back to the database
	// if not found in cache.
	// Returns the record as a map[string]interface{} or an error if not found.
	Read(ctx context.Context, key interface{}) (map[string]interface{}, error)

	// Update modifies an existing record identified by the primary key.
	// Updates are written to the KV store first, then asynchronously
	// written back to the database.
	// The updates map should contain only the fields to be updated.
	Update(ctx context.Context, key interface{}, updates map[string]interface{}) error

	// Delete removes a record by its primary key.
	// Deletions are written to the KV store first, then asynchronously
	// written back to the database.
	Delete(ctx context.Context, key interface{}) error

	// EnableKV enables KV store caching for this table.
	// This must be called before using the table for KV operations.
	// Note: This is typically called automatically when using Client.EnableKV().
	EnableKV() error

	// DisableKV disables KV store caching for this table.
	// After disabling, operations will go directly to the database.
	// Note: This is typically called via Client.DisableKV().
	DisableKV() error

	// FindByField queries the database for a record by a non-primary key field.
	// This is useful for querying by fields like reference_id.
	// It first queries the database to find the primary key, then reads the record
	// (which will check Redis cache first if KV is enabled).
	FindByField(ctx context.Context, fieldName string, fieldValue interface{}) (map[string]interface{}, error)
}

// tableWrapper wraps the internal core.Table interface to provide
// the public Table interface.
type tableWrapper struct {
	table core.Table
}

// Create inserts a new record into the table.
func (tw *tableWrapper) Create(ctx context.Context, record map[string]interface{}) error {
	return tw.table.Create(ctx, record)
}

// Read retrieves a record by its primary key.
func (tw *tableWrapper) Read(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	return tw.table.Read(ctx, key)
}

// Update modifies an existing record identified by the primary key.
func (tw *tableWrapper) Update(ctx context.Context, key interface{}, updates map[string]interface{}) error {
	return tw.table.Update(ctx, key, updates)
}

// Delete removes a record by its primary key.
func (tw *tableWrapper) Delete(ctx context.Context, key interface{}) error {
	return tw.table.Delete(ctx, key)
}

// EnableKV enables KV store caching for this table.
func (tw *tableWrapper) EnableKV() error {
	return tw.table.EnableKV()
}

// DisableKV disables KV store caching for this table.
func (tw *tableWrapper) DisableKV() error {
	return tw.table.DisableKV()
}

// FindByField queries the database for a record by a non-primary key field.
func (tw *tableWrapper) FindByField(ctx context.Context, fieldName string, fieldValue interface{}) (map[string]interface{}, error) {
	return tw.table.FindByField(ctx, fieldName, fieldValue)
}

// GetInternalTable is a helper method for testing/debugging that returns the internal core.Table.
// This should only be used in test code, not in production.
func (tw *tableWrapper) GetInternalTable() interface{} {
	return tw.table
}
