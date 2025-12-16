package core

import (
	"context"
	"time"
)

// OperationType represents the type of write operation.
type OperationType string

const (
	// OperationCreate represents an INSERT operation.
	OperationCreate OperationType = "CREATE"

	// OperationUpdate represents an UPDATE operation.
	OperationUpdate OperationType = "UPDATE"

	// OperationDelete represents a DELETE operation.
	OperationDelete OperationType = "DELETE"
)

// WriteOperation represents a single write operation that needs to be
// written back from the KV store to the persistent database.
type WriteOperation struct {
	// Table is the name of the table this operation targets.
	Table string

	// Operation is the type of operation (CREATE, UPDATE, DELETE).
	Operation OperationType

	// Key is the primary key value for the record.
	Key interface{}

	// Data contains the record data for CREATE and UPDATE operations.
	// For DELETE operations, this may be nil or contain metadata.
	Data map[string]interface{}

	// Timestamp is when the operation was originally performed.
	Timestamp time.Time

	// RetryCount tracks how many times this operation has been retried.
	RetryCount int
}

// WriteBackQueue defines the interface for managing write-back operations.
// The queue stores operations that have been written to the KV store
// and need to be asynchronously written back to the persistent database.
type WriteBackQueue interface {
	// Enqueue adds a write operation to the queue.
	// Operations are typically enqueued after successful writes to the KV store.
	Enqueue(ctx context.Context, operation *WriteOperation) error

	// Dequeue retrieves a batch of write operations from the queue.
	// The batchSize parameter controls how many operations to retrieve.
	// Returns an empty slice if no operations are available.
	Dequeue(ctx context.Context, batchSize int) ([]*WriteOperation, error)

	// Size returns the current number of operations in the queue.
	Size() int

	// Close closes the queue and releases resources.
	Close() error
}

