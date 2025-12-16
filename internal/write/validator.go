package write

import (
	"errors"
	"fmt"

	"github.com/razorpay/shock-absorber/internal/core"
)

var (
	// ErrInvalidRecord is returned when a record doesn't match the expected schema.
	ErrInvalidRecord = errors.New("invalid record")

	// ErrMissingPrimaryKey is returned when a record is missing the primary key.
	ErrMissingPrimaryKey = errors.New("missing primary key")

	// ErrInvalidOperationType is returned when an invalid operation type is provided.
	ErrInvalidOperationType = errors.New("invalid operation type")
)

// WriteValidator validates write operations against table schemas.
type WriteValidator struct {
	schema *core.Schema
}

// NewWriteValidator creates a new write validator for a specific schema.
func NewWriteValidator(schema *core.Schema) *WriteValidator {
	return &WriteValidator{
		schema: schema,
	}
}

// ValidateCreate validates a CREATE operation.
func (v *WriteValidator) ValidateCreate(record map[string]interface{}) error {
	if record == nil {
		return fmt.Errorf("%w: record cannot be nil", ErrInvalidRecord)
	}

	// Check for primary key
	if v.schema.PrimaryKey != "" {
		if _, exists := record[v.schema.PrimaryKey]; !exists {
			return fmt.Errorf("%w: primary key '%s' is required", ErrMissingPrimaryKey, v.schema.PrimaryKey)
		}
	}

	// Validate column types and constraints
	return v.validateColumns(record)
}

// ValidateUpdate validates an UPDATE operation.
func (v *WriteValidator) ValidateUpdate(key interface{}, updates map[string]interface{}) error {
	if key == nil {
		return fmt.Errorf("%w: key cannot be nil", ErrInvalidRecord)
	}

	if updates == nil || len(updates) == 0 {
		return fmt.Errorf("%w: updates cannot be empty", ErrInvalidRecord)
	}

	// Validate that we're not trying to update the primary key
	if v.schema.PrimaryKey != "" {
		if _, exists := updates[v.schema.PrimaryKey]; exists {
			return fmt.Errorf("%w: cannot update primary key '%s'", ErrInvalidRecord, v.schema.PrimaryKey)
		}
	}

	// Validate column types and constraints for updated fields
	return v.validateColumns(updates)
}

// ValidateDelete validates a DELETE operation.
func (v *WriteValidator) ValidateDelete(key interface{}) error {
	if key == nil {
		return fmt.Errorf("%w: key cannot be nil", ErrInvalidRecord)
	}
	return nil
}

// validateColumns validates that the provided record fields match the schema.
func (v *WriteValidator) validateColumns(record map[string]interface{}) error {
	if v.schema == nil {
		return nil // No schema to validate against
	}

	// Check each field in the record against the schema
	for fieldName, fieldValue := range record {
		// Find the column definition
		var column *core.Column
		for i := range v.schema.Columns {
			if v.schema.Columns[i].Name == fieldName {
				column = &v.schema.Columns[i]
				break
			}
		}

		// If column not found in schema, that's okay - might be a new field
		// In strict mode, you might want to return an error here
		if column == nil {
			continue
		}

		// Check NULL constraint
		if fieldValue == nil {
			if !column.Nullable {
				return fmt.Errorf("%w: column '%s' cannot be NULL", ErrInvalidRecord, fieldName)
			}
			continue
		}

		// Basic type validation
		// In a full implementation, you'd do more sophisticated type checking
		// based on the column.Type (e.g., INT, VARCHAR, TIMESTAMP, etc.)
		if err := v.validateType(fieldName, fieldValue, column); err != nil {
			return err
		}
	}

	return nil
}

// validateType performs basic type validation.
// This is a simplified implementation - a full version would handle
// all database types and conversions.
func (v *WriteValidator) validateType(fieldName string, value interface{}, column *core.Column) error {
	// Basic type checking based on common patterns
	// In production, you'd want a more comprehensive type system
	switch column.Type {
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT":
		// Check if value is numeric
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return nil
		default:
			return fmt.Errorf("%w: column '%s' expects numeric type, got %T", ErrInvalidRecord, fieldName, value)
		}
	case "VARCHAR", "TEXT", "CHAR":
		// Check if value is string-like
		switch value.(type) {
		case string, []byte:
			return nil
		default:
			return fmt.Errorf("%w: column '%s' expects string type, got %T", ErrInvalidRecord, fieldName, value)
		}
	case "TIMESTAMP", "DATETIME", "DATE":
		// Check if value is time-like
		switch value.(type) {
		case string, int64: // string for formatted dates, int64 for Unix timestamps
			return nil
		default:
			// Try to see if it's a time.Time
			if _, ok := value.(interface{ Unix() int64 }); ok {
				return nil
			}
			return fmt.Errorf("%w: column '%s' expects timestamp type, got %T", ErrInvalidRecord, fieldName, value)
		}
	case "BOOLEAN", "BOOL", "TINYINT(1)":
		// Check if value is boolean-like
		switch value.(type) {
		case bool, int, int8: // MySQL uses TINYINT(1) for booleans
			return nil
		default:
			return fmt.Errorf("%w: column '%s' expects boolean type, got %T", ErrInvalidRecord, fieldName, value)
		}
	default:
		// For unknown types, accept any value
		// In production, you might want stricter validation
		return nil
	}
}

// ValidateOperationType validates that an operation type is valid.
func ValidateOperationType(opType core.OperationType) error {
	switch opType {
	case core.OperationCreate, core.OperationUpdate, core.OperationDelete:
		return nil
	default:
		return fmt.Errorf("%w: '%s'", ErrInvalidOperationType, opType)
	}
}

