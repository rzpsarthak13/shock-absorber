package schema

import (
	"fmt"

	"github.com/razorpay/shock-absorber/internal/core"
)

// SchemaValidator validates records against schema definitions.
type SchemaValidator struct {
	schema *core.Schema
	mapper *TypeMapper
}

// NewSchemaValidator creates a new schema validator.
func NewSchemaValidator(schema *core.Schema) *SchemaValidator {
	return &SchemaValidator{
		schema: schema,
		mapper: NewTypeMapper(),
	}
}

// ValidateRecord validates a record against the schema.
// Returns an error if the record doesn't match the schema requirements.
func (sv *SchemaValidator) ValidateRecord(record map[string]interface{}) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if sv.schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	// Validate primary key exists
	if sv.schema.PrimaryKey != "" {
		if _, exists := record[sv.schema.PrimaryKey]; !exists {
			return fmt.Errorf("missing required primary key: %s", sv.schema.PrimaryKey)
		}
	}

	// Validate each column
	for _, column := range sv.schema.Columns {
		value, exists := record[column.Name]
		
		// Check NULL constraint
		if !exists || value == nil {
			if !column.Nullable {
				return fmt.Errorf("column '%s' cannot be NULL", column.Name)
			}
			continue
		}

		// Validate type compatibility
		if err := sv.validateColumnType(column, value); err != nil {
			return fmt.Errorf("column '%s': %w", column.Name, err)
		}
	}

	return nil
}

// ValidatePartialRecord validates a partial record (e.g., for UPDATE operations).
// Only validates fields that are present in the record.
func (sv *SchemaValidator) ValidatePartialRecord(record map[string]interface{}) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if sv.schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	// Validate each field that exists in the record
	for fieldName, fieldValue := range record {
		// Find the column definition
		var column *core.Column
		for i := range sv.schema.Columns {
			if sv.schema.Columns[i].Name == fieldName {
				column = &sv.schema.Columns[i]
				break
			}
		}

		// If column not found, that's okay for partial records (might be a new field)
		if column == nil {
			continue
		}

		// Check NULL constraint
		if fieldValue == nil {
			if !column.Nullable {
				return fmt.Errorf("column '%s' cannot be NULL", fieldName)
			}
			continue
		}

		// Validate type compatibility
		if err := sv.validateColumnType(*column, fieldValue); err != nil {
			return fmt.Errorf("column '%s': %w", fieldName, err)
		}
	}

	return nil
}

// validateColumnType validates that a value is compatible with the column type.
func (sv *SchemaValidator) validateColumnType(column core.Column, value interface{}) error {
	// Try to convert the value to the expected DB type
	// If conversion succeeds, the type is compatible
	_, err := sv.mapper.ConvertToDBValue(value, column.Type)
	if err != nil {
		return fmt.Errorf("type mismatch: expected %s, got %T: %w", column.Type, value, err)
	}

	return nil
}

// ValidatePrimaryKey validates that a primary key value is valid.
func (sv *SchemaValidator) ValidatePrimaryKey(key interface{}) error {
	if key == nil {
		return fmt.Errorf("primary key cannot be nil")
	}

	if sv.schema == nil || sv.schema.PrimaryKey == "" {
		return fmt.Errorf("schema has no primary key defined")
	}

	// Find the primary key column
	var pkColumn *core.Column
	for i := range sv.schema.Columns {
		if sv.schema.Columns[i].Name == sv.schema.PrimaryKey {
			pkColumn = &sv.schema.Columns[i]
			break
		}
	}

	if pkColumn == nil {
		return fmt.Errorf("primary key column '%s' not found in schema", sv.schema.PrimaryKey)
	}

	// Validate the key type
	return sv.validateColumnType(*pkColumn, key)
}

