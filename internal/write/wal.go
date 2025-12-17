package write

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// WALOperation represents a write-ahead log entry.
type WALOperation struct {
	// OperationID is a unique identifier for this operation.
	OperationID string

	// Table is the name of the table this operation targets.
	Table string

	// Operation is the type of operation (CREATE, UPDATE, DELETE).
	Operation core.OperationType

	// Key is the primary key value for the record.
	Key interface{}

	// Data contains the record data for CREATE and UPDATE operations.
	// For DELETE operations, this may be nil or contain metadata.
	Data map[string]interface{}

	// Timestamp is when the operation was performed.
	Timestamp time.Time

	// KeyValue is the KV store key where the data is stored.
	KeyValue string

	// Value is the serialized value stored in the KV store.
	Value []byte
}

// WALManager manages the write-ahead log in Redis.
// All writes are first recorded in the WAL before being acknowledged.
type WALManager struct {
	kvStore core.KVStore
	prefix  string
}

// NewWALManager creates a new WAL manager.
// prefix is used to namespace WAL entries in Redis (e.g., "wal:").
func NewWALManager(kvStore core.KVStore, prefix string) *WALManager {
	if prefix == "" {
		prefix = "wal"
	}
	return &WALManager{
		kvStore: kvStore,
		prefix:  prefix,
	}
}

// Append appends a write operation to the WAL.
// This generates a unique operation ID and stores the operation metadata in Redis.
// The operation is stored in an append-only log pattern using Redis Lists or Streams.
func (w *WALManager) Append(ctx context.Context, op *WALOperation) error {
	if op.OperationID == "" {
		op.OperationID = w.generateOperationID(op.Table, op.Key)
	}

	if op.Timestamp.IsZero() {
		op.Timestamp = time.Now()
	}

	// Serialize the operation metadata
	opData, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL operation: %w", err)
	}

	// Store the operation entry
	// Format: wal:{table}:entry:{operation_id}
	walEntryKey := fmt.Sprintf("%s:%s:entry:%s", w.prefix, op.Table, op.OperationID)
	
	// Store the operation entry with a TTL (e.g., 24 hours for audit trail)
	ttl := 24 * time.Hour
	if err := w.kvStore.Set(ctx, walEntryKey, opData, ttl); err != nil {
		return fmt.Errorf("failed to store WAL entry: %w", err)
	}

	// Maintain a timestamp-based index for ordering and retrieval
	// Format: wal:{table}:timestamp:{timestamp_nano}:{operation_id}
	// This allows us to retrieve operations in order
	timestampKey := fmt.Sprintf("%s:%s:timestamp:%d:%s", w.prefix, op.Table, op.Timestamp.UnixNano(), op.OperationID)
	if err := w.kvStore.Set(ctx, timestampKey, []byte(op.OperationID), ttl); err != nil {
		return fmt.Errorf("failed to store WAL timestamp index: %w", err)
	}

	return nil
}

// Get retrieves a WAL operation by its ID.
func (w *WALManager) Get(ctx context.Context, table string, operationID string) (*WALOperation, error) {
	walEntryKey := fmt.Sprintf("%s:%s:entry:%s", w.prefix, table, operationID)
	
	data, err := w.kvStore.Get(ctx, walEntryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get WAL entry: %w", err)
	}

	var op WALOperation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, fmt.Errorf("failed to unmarshal WAL operation: %w", err)
	}

	return &op, nil
}

// Acknowledge marks a WAL operation as acknowledged (written back to DB).
// This can be used to clean up old WAL entries after successful write-back.
func (w *WALManager) Acknowledge(ctx context.Context, table string, operationID string) error {
	// Mark the operation as acknowledged
	ackKey := fmt.Sprintf("%s:%s:ack:%s", w.prefix, table, operationID)
	if err := w.kvStore.Set(ctx, ackKey, []byte("1"), 7*24*time.Hour); err != nil {
		return fmt.Errorf("failed to acknowledge WAL entry: %w", err)
	}
	return nil
}

// IsAcknowledged checks if a WAL operation has been acknowledged.
func (w *WALManager) IsAcknowledged(ctx context.Context, table string, operationID string) (bool, error) {
	ackKey := fmt.Sprintf("%s:%s:ack:%s", w.prefix, table, operationID)
	exists, err := w.kvStore.Exists(ctx, ackKey)
	if err != nil {
		return false, fmt.Errorf("failed to check WAL acknowledgment: %w", err)
	}
	return exists, nil
}

// generateOperationID generates a unique operation ID.
// Format: {table}_{key}_{timestamp_nano}
func (w *WALManager) generateOperationID(table string, key interface{}) string {
	return fmt.Sprintf("%s_%v_%d", table, key, time.Now().UnixNano())
}
