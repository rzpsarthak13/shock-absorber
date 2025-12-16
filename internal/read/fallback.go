package read

import (
	"context"
	"fmt"
	"time"

	"github.com/razorpay/shock-absorber/internal/core"
)

// FallbackHandler handles database fallback when cache misses occur.
// It queries the database, populates the cache, and handles concurrent reads.
type FallbackHandler struct {
	database   core.Database
	kvStore    core.KVStore
	translator core.SchemaTranslator
	keyBuilder *KeyBuilder
	ttl        time.Duration
}

// NewFallbackHandler creates a new fallback handler.
func NewFallbackHandler(
	database core.Database,
	kvStore core.KVStore,
	translator core.SchemaTranslator,
	namespace string,
	ttl time.Duration,
) *FallbackHandler {
	return &FallbackHandler{
		database:   database,
		kvStore:    kvStore,
		translator: translator,
		keyBuilder: NewKeyBuilder(namespace),
		ttl:        ttl,
	}
}

// ReadFromDB reads a record from the database by primary key.
// If successful, it populates the cache with the result.
func (fh *FallbackHandler) ReadFromDB(ctx context.Context, tableName string, key interface{}, schema *core.Schema) (map[string]interface{}, error) {
	// Build the SQL query
	query := fh.buildSelectQuery(tableName, schema)

	// Execute the query
	rows, err := fh.database.Query(ctx, query, key)
	if err != nil {
		return nil, fmt.Errorf("database query failed: %w", err)
	}
	defer rows.Close()

	// Check if any rows were returned
	if !rows.Next() {
		return nil, fmt.Errorf("record not found: table=%s, key=%v", tableName, key)
	}

	// Scan the row into a record map
	record, err := fh.scanRow(rows, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	// Populate cache asynchronously (don't block on cache write)
	go fh.populateCache(context.Background(), tableName, key, record, schema)

	return record, nil
}

// populateCache populates the cache with a record from the database.
// This is called after a successful DB read to warm the cache.
func (fh *FallbackHandler) populateCache(ctx context.Context, tableName string, key interface{}, record map[string]interface{}, schema *core.Schema) error {
	// Convert record to KV format
	cacheKey, value, err := fh.translator.ToKV(record, schema)
	if err != nil {
		// Log error but don't fail the read operation
		return fmt.Errorf("failed to convert record to KV: %w", err)
	}

	// Set in cache with TTL
	if err := fh.kvStore.Set(ctx, cacheKey, value, fh.ttl); err != nil {
		// Log error but don't fail the read operation
		return fmt.Errorf("failed to populate cache: %w", err)
	}

	return nil
}

// buildSelectQuery builds a SELECT query for a table by primary key.
func (fh *FallbackHandler) buildSelectQuery(tableName string, schema *core.Schema) string {
	// Build column list
	columns := make([]string, 0, len(schema.Columns))
	for _, col := range schema.Columns {
		columns = append(columns, col.Name)
	}

	// Build the query
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		joinColumns(columns),
		tableName,
		schema.PrimaryKey,
	)

	return query
}

// scanRow scans a database row into a record map using the schema.
// It builds the record map directly from scanned values.
// The database driver handles basic type conversion, and the translator
// will handle further conversion when converting to KV format.
func (fh *FallbackHandler) scanRow(rows core.Rows, schema *core.Schema) (map[string]interface{}, error) {
	// Create a slice of interface{} pointers for scanning
	// The database driver will populate these with the column values
	values := make([]interface{}, len(schema.Columns))
	valuePtrs := make([]interface{}, len(schema.Columns))

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Scan the row - the database driver handles type conversion
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Build the record map directly from scanned values
	// Map column names to their values
	record := make(map[string]interface{}, len(schema.Columns))
	for i, col := range schema.Columns {
		record[col.Name] = values[i]
	}

	return record, nil
}

// joinColumns joins column names with commas.
func joinColumns(columns []string) string {
	if len(columns) == 0 {
		return "*"
	}

	result := columns[0]
	for i := 1; i < len(columns); i++ {
		result += ", " + columns[i]
	}
	return result
}
