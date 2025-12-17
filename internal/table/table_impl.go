package table

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
	"github.com/rzpsarthak13/shock-absorber/internal/read"
	schematranslator "github.com/rzpsarthak13/shock-absorber/internal/schema"
	"github.com/rzpsarthak13/shock-absorber/internal/write"
)

// TableImpl implements the core.Table interface.
type TableImpl struct {
	mu              sync.RWMutex
	tableName       string
	schema          *core.Schema
	kvStore         core.KVStore
	database        core.Database
	translator      core.SchemaTranslator
	walManager      *write.WALManager
	cacheHandler    *read.CacheHandler
	fallbackHandler *read.FallbackHandler
	writeQueue      core.WriteBackQueue
	enabled         bool
	ttl             time.Duration
	namespace       string
}

// NewTableImpl creates a new table implementation.
func NewTableImpl(
	tableName string,
	schema *core.Schema,
	kvStore core.KVStore,
	database core.Database,
	writeQueue core.WriteBackQueue,
	ttl time.Duration,
	namespace string,
) *TableImpl {
	translator := schematranslator.NewTranslator()
	walManager := write.NewWALManager(kvStore, "wal")
	cacheHandler := read.NewCacheHandler(kvStore, translator, namespace)
	fallbackHandler := read.NewFallbackHandler(database, kvStore, translator, namespace, ttl)

	return &TableImpl{
		tableName:       tableName,
		schema:          schema,
		kvStore:         kvStore,
		database:        database,
		translator:      translator,
		walManager:      walManager,
		cacheHandler:    cacheHandler,
		fallbackHandler: fallbackHandler,
		writeQueue:      writeQueue,
		enabled:         false,
		ttl:             ttl,
		namespace:       namespace,
	}
}

// Create inserts a new record into the table.
func (t *TableImpl) Create(ctx context.Context, record map[string]interface{}) error {
	log.Printf("[TABLE] === CREATE OPERATION START ===")
	log.Printf("[TABLE] Table: %s", t.tableName)

	t.mu.RLock()
	enabled := t.enabled
	t.mu.RUnlock()

	if !enabled {
		log.Printf("[TABLE] KV disabled, writing directly to database")
		return t.writeToDatabase(ctx, core.OperationCreate, nil, record)
	}

	log.Printf("[TABLE] Step 1: Converting record to KV format")
	// Write to KV store first (WAL)
	key, value, err := t.translator.ToKV(record, t.schema)
	if err != nil {
		log.Printf("[TABLE] ERROR: Failed to convert record to KV: %v", err)
		return fmt.Errorf("failed to convert record to KV: %w", err)
	}
	log.Printf("[TABLE] Step 1 Complete: Redis Key = %s, Value Size = %d bytes", key, len(value))
	log.Printf("[TABLE] Redis Key Format: {table}:{primary_key} = %s", key)

	// Log a preview of the value stored in Redis
	var valuePreview map[string]interface{}
	if err := json.Unmarshal(value, &valuePreview); err == nil {
		log.Printf("[TABLE] Redis Value Preview (first 500 chars): %+v", valuePreview)
	}

	// Store in KV store
	log.Printf("[TABLE] Step 2: Writing to Redis cache (key: %s, TTL: %v)", key, t.ttl)
	if err := t.kvStore.Set(ctx, key, value, t.ttl); err != nil {
		log.Printf("[TABLE] ERROR: Failed to write to KV store: %v", err)
		return fmt.Errorf("failed to write to KV store: %w", err)
	}
	log.Printf("[TABLE] Step 2 Complete: Successfully written to Redis")

	// Append to WAL
	pkValue := record[t.schema.PrimaryKey]
	log.Printf("[TABLE] Step 3: Appending to Write-Ahead Log (WAL)")
	walOp := &write.WALOperation{
		Table:     t.tableName,
		Operation: core.OperationCreate,
		Key:       pkValue,
		Data:      record,
		Timestamp: time.Now(),
		KeyValue:  key,
		Value:     value,
	}
	if err := t.walManager.Append(ctx, walOp); err != nil {
		log.Printf("[TABLE] ERROR: Failed to append to WAL: %v", err)
		return fmt.Errorf("failed to append to WAL: %w", err)
	}
	log.Printf("[TABLE] Step 3 Complete: WAL entry created (OperationID: %s)", walOp.OperationID)

	// Enqueue for write-back
	log.Printf("[TABLE] Step 4: Enqueueing operation for async write-back to database")
	writeOp := &core.WriteOperation{
		Table:     t.tableName,
		Operation: core.OperationCreate,
		Key:       pkValue,
		Data:      record,
		Timestamp: time.Now(),
	}
	if err := t.writeQueue.Enqueue(ctx, writeOp); err != nil {
		log.Printf("[TABLE] ERROR: Failed to enqueue write operation: %v", err)
		return fmt.Errorf("failed to enqueue write operation: %w", err)
	}
	log.Printf("[TABLE] Step 4 Complete: Operation enqueued (queue size: %d)", t.writeQueue.Size())
	log.Printf("[TABLE] CREATE operation complete - data in Redis, queued for DB write-back")
	log.Printf("[TABLE] === CREATE OPERATION END ===")

	return nil
}

// Read retrieves a record by its primary key.
func (t *TableImpl) Read(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	log.Printf("[TABLE] === READ OPERATION START ===")
	log.Printf("[TABLE] Table: %s, Key: %v", t.tableName, key)

	t.mu.RLock()
	enabled := t.enabled
	t.mu.RUnlock()

	if !enabled {
		log.Printf("[TABLE] KV disabled, reading directly from database")
		return t.readFromDatabase(ctx, key)
	}

	// Try cache first
	log.Printf("[TABLE] Step 1: Checking Redis cache")
	record, err := t.cacheHandler.Read(ctx, t.tableName, key, t.schema)
	if err == nil {
		log.Printf("[TABLE] Step 1 Complete: Cache HIT - record found in Redis")
		log.Printf("[TABLE] === READ OPERATION END (Cache Hit) ===")
		return record, nil
	}
	log.Printf("[TABLE] Step 1 Result: Cache MISS - %v", err)

	// Cache miss - fallback to database
	log.Printf("[TABLE] Step 2: Falling back to database")
	record, err = t.fallbackHandler.ReadFromDB(ctx, t.tableName, key, t.schema)
	if err != nil {
		log.Printf("[TABLE] ERROR: Failed to read from database: %v", err)
		return nil, err
	}
	log.Printf("[TABLE] Step 2 Complete: Record found in database and cached in Redis")
	log.Printf("[TABLE] === READ OPERATION END (DB Fallback) ===")
	return record, nil
}

// Update modifies an existing record.
func (t *TableImpl) Update(ctx context.Context, key interface{}, updates map[string]interface{}) error {
	t.mu.RLock()
	enabled := t.enabled
	t.mu.RUnlock()

	if !enabled {
		// If KV is disabled, update directly in database
		return t.writeToDatabase(ctx, core.OperationUpdate, key, updates)
	}

	// Read current record
	record, err := t.Read(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to read record for update: %w", err)
	}

	// Merge updates
	for k, v := range updates {
		record[k] = v
	}

	// Write to KV store
	kvKey, value, err := t.translator.ToKV(record, t.schema)
	if err != nil {
		return fmt.Errorf("failed to convert record to KV: %w", err)
	}

	if err := t.kvStore.Set(ctx, kvKey, value, t.ttl); err != nil {
		return fmt.Errorf("failed to write to KV store: %w", err)
	}

	// Append to WAL
	walOp := &write.WALOperation{
		Table:     t.tableName,
		Operation: core.OperationUpdate,
		Key:       key,
		Data:      updates,
		Timestamp: time.Now(),
		KeyValue:  kvKey,
		Value:     value,
	}
	if err := t.walManager.Append(ctx, walOp); err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Enqueue for write-back
	writeOp := &core.WriteOperation{
		Table:     t.tableName,
		Operation: core.OperationUpdate,
		Key:       key,
		Data:      updates,
		Timestamp: time.Now(),
	}
	if err := t.writeQueue.Enqueue(ctx, writeOp); err != nil {
		return fmt.Errorf("failed to enqueue write operation: %w", err)
	}

	return nil
}

// Delete removes a record by its primary key.
func (t *TableImpl) Delete(ctx context.Context, key interface{}) error {
	t.mu.RLock()
	enabled := t.enabled
	t.mu.RUnlock()

	if !enabled {
		// If KV is disabled, delete directly from database
		return t.writeToDatabase(ctx, core.OperationDelete, key, nil)
	}

	// Build cache key
	keyBuilder := read.NewKeyBuilder(t.namespace)
	cacheKey := keyBuilder.BuildKey(t.tableName, key)

	// Delete from KV store
	if err := t.kvStore.Delete(ctx, cacheKey); err != nil {
		return fmt.Errorf("failed to delete from KV store: %w", err)
	}

	// Append to WAL
	walOp := &write.WALOperation{
		Table:     t.tableName,
		Operation: core.OperationDelete,
		Key:       key,
		Data:      nil,
		Timestamp: time.Now(),
		KeyValue:  cacheKey,
		Value:     nil,
	}
	if err := t.walManager.Append(ctx, walOp); err != nil {
		return fmt.Errorf("failed to append to WAL: %w", err)
	}

	// Enqueue for write-back
	writeOp := &core.WriteOperation{
		Table:     t.tableName,
		Operation: core.OperationDelete,
		Key:       key,
		Data:      nil,
		Timestamp: time.Now(),
	}
	if err := t.writeQueue.Enqueue(ctx, writeOp); err != nil {
		return fmt.Errorf("failed to enqueue write operation: %w", err)
	}

	return nil
}

// EnableKV enables KV store caching for this table.
func (t *TableImpl) EnableKV() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = true
	return nil
}

// DisableKV disables KV store caching for this table.
func (t *TableImpl) DisableKV() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = false
	return nil
}

// writeToDatabase writes directly to the database (used when KV is disabled).
func (t *TableImpl) writeToDatabase(ctx context.Context, opType core.OperationType, key interface{}, data map[string]interface{}) error {
	log.Printf("[TABLE] writeToDatabase: Operation=%s, Table=%s, Key=%v", opType, t.tableName, key)
	translator := t.translator.(*schematranslator.Translator)

	switch opType {
	case core.OperationCreate:
		log.Printf("[TABLE] Converting data to DB format for CREATE operation")
		query, args, err := translator.ToDB(data, t.schema)
		if err != nil {
			log.Printf("[TABLE] ERROR: Failed to convert data to DB format: %v", err)
			return err
		}
		log.Printf("[TABLE] Executing CREATE query: %s", query)
		log.Printf("[TABLE] Query args: %+v", args)
		result, err := t.database.Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[TABLE] ERROR: Database Exec failed: %v", err)
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Printf("[TABLE] WARNING: Could not get rows affected: %v", err)
		} else {
			log.Printf("[TABLE] CREATE successful, rows affected: %d", rowsAffected)
		}
		return nil

	case core.OperationUpdate:
		log.Printf("[TABLE] Converting data to DB format for UPDATE operation")
		query, args, err := translator.ToDBUpdate(key, data, t.schema)
		if err != nil {
			log.Printf("[TABLE] ERROR: Failed to convert data to DB format: %v", err)
			return err
		}
		log.Printf("[TABLE] Executing UPDATE query: %s", query)
		log.Printf("[TABLE] Query args: %+v", args)
		result, err := t.database.Exec(ctx, query, args...)
		if err != nil {
			log.Printf("[TABLE] ERROR: Database Exec failed: %v", err)
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Printf("[TABLE] WARNING: Could not get rows affected: %v", err)
		} else {
			log.Printf("[TABLE] UPDATE successful, rows affected: %d", rowsAffected)
		}
		return nil

	case core.OperationDelete:
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", t.tableName, t.schema.PrimaryKey)
		log.Printf("[TABLE] Executing DELETE query: %s with key: %v", query, key)
		result, err := t.database.Exec(ctx, query, key)
		if err != nil {
			log.Printf("[TABLE] ERROR: Database Exec failed: %v", err)
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Printf("[TABLE] WARNING: Could not get rows affected: %v", err)
		} else {
			log.Printf("[TABLE] DELETE successful, rows affected: %d", rowsAffected)
		}
		return nil

	default:
		log.Printf("[TABLE] ERROR: Unsupported operation type: %s", opType)
		return fmt.Errorf("unsupported operation type: %s", opType)
	}
}

// readFromDatabase reads directly from the database (used when KV is disabled).
func (t *TableImpl) readFromDatabase(ctx context.Context, key interface{}) (map[string]interface{}, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s = ?", t.tableName, t.schema.PrimaryKey)
	rows, err := t.database.Query(ctx, query, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("record not found")
	}

	record, err := t.translator.FromDB(rows, t.schema)
	if err != nil {
		return nil, err
	}

	return record, nil
}

// FindByField queries the database for a record by a non-primary key field.
// This is a helper method for querying by fields like reference_id.
// It first checks if the record exists in Redis by querying DB for the ID, then reads from cache.
func (t *TableImpl) FindByField(ctx context.Context, fieldName string, fieldValue interface{}) (map[string]interface{}, error) {
	log.Printf("[TABLE] Finding record by field: %s = %v", fieldName, fieldValue)

	// Query database to find the primary key
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? LIMIT 1", t.schema.PrimaryKey, t.tableName, fieldName)
	log.Printf("[TABLE] Executing query to find primary key: %s with value: %v", query, fieldValue)

	rows, err := t.database.Query(ctx, query, fieldValue)
	if err != nil {
		log.Printf("[TABLE] ERROR: Query failed: %v", err)
		return nil, fmt.Errorf("failed to query database: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		log.Printf("[TABLE] No record found with %s = %v", fieldName, fieldValue)
		return nil, fmt.Errorf("record not found with %s = %v", fieldName, fieldValue)
	}

	var pkValue interface{}
	if err := rows.Scan(&pkValue); err != nil {
		log.Printf("[TABLE] ERROR: Failed to scan primary key: %v", err)
		return nil, fmt.Errorf("failed to scan primary key: %w", err)
	}

	log.Printf("[TABLE] Found primary key: %v, now reading record", pkValue)

	// Now read the full record using the primary key (this will check Redis first)
	return t.Read(ctx, pkValue)
}

// ExecuteWriteOperation executes a write operation directly to the database.
// This bypasses the KV store and is used by the write-back drainer.
func (t *TableImpl) ExecuteWriteOperation(ctx context.Context, operation *core.WriteOperation) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	log.Printf("[TABLE] [%s] === EXECUTE WRITE OPERATION START ===", timestamp)
	log.Printf("[TABLE] [%s] Operation: %s, Table: %s, Key: %v", timestamp, operation.Operation, operation.Table, operation.Key)
	log.Printf("[TABLE] [%s] Data: %+v", timestamp, operation.Data)

	writeStart := time.Now()
	err := t.writeToDatabase(ctx, operation.Operation, operation.Key, operation.Data)
	writeDuration := time.Since(writeStart)

	if err != nil {
		log.Printf("[TABLE] [%s] ERROR: Failed to execute write operation: %v (Duration: %v)",
			time.Now().Format("2006-01-02 15:04:05.000"), err, writeDuration)
		log.Printf("[TABLE] [%s] === EXECUTE WRITE OPERATION FAILED ===",
			time.Now().Format("2006-01-02 15:04:05.000"))
		return err
	}

	log.Printf("[TABLE] [%s] ✓ Successfully executed write operation to database (Duration: %v)",
		time.Now().Format("2006-01-02 15:04:05.000"), writeDuration)
	log.Printf("[TABLE] [%s] === EXECUTE WRITE OPERATION END ===",
		time.Now().Format("2006-01-02 15:04:05.000"))
	return nil
}

// GetWriteBackQueue returns the write-back queue for this table.
func (t *TableImpl) GetWriteBackQueue() core.WriteBackQueue {
	return t.writeQueue
}
