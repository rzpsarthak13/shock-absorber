package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// Translator implements the SchemaTranslator interface.
// It handles conversion between relational database records and key-value store formats.
type Translator struct {
	mapper    *TypeMapper
	keyFormat string // Format for building keys: "{table}:{primary_key}"
}

// NewTranslator creates a new schema translator.
func NewTranslator() *Translator {
	return &Translator{
		mapper:    NewTypeMapper(),
		keyFormat: "%s:%v", // Default format: table:primary_key
	}
}

// NewTranslatorWithKeyFormat creates a new schema translator with a custom key format.
// The format should contain two placeholders: first for table name, second for primary key value.
func NewTranslatorWithKeyFormat(format string) *Translator {
	return &Translator{
		mapper:    NewTypeMapper(),
		keyFormat: format,
	}
}

// ToKV converts a relational record to a key-value pair.
// Returns the key (typically table:primary_key_value) and the serialized value.
func (t *Translator) ToKV(record map[string]interface{}, schema *core.Schema) (string, []byte, error) {
	if record == nil {
		return "", nil, fmt.Errorf("record cannot be nil")
	}

	if schema == nil {
		return "", nil, fmt.Errorf("schema cannot be nil")
	}

	// Validate the record
	validator := NewSchemaValidator(schema)
	if err := validator.ValidateRecord(record); err != nil {
		return "", nil, fmt.Errorf("validation failed: %w", err)
	}

	// Extract primary key value
	pkValue, exists := record[schema.PrimaryKey]
	if !exists {
		return "", nil, fmt.Errorf("primary key '%s' not found in record", schema.PrimaryKey)
	}

	// Build the key
	key := fmt.Sprintf(t.keyFormat, schema.TableName, pkValue)

	// Convert record values to DB-compatible types for serialization
	kvRecord := make(map[string]interface{}, len(record))
	for colName, value := range record {
		// Find the column definition to get the DB type
		var dbType string
		for _, col := range schema.Columns {
			if col.Name == colName {
				dbType = col.Type
				break
			}
		}

		// Convert value to appropriate type
		if value != nil {
			converted, err := t.mapper.ConvertToDBValue(value, dbType)
			if err != nil {
				return "", nil, fmt.Errorf("failed to convert value for column '%s': %w", colName, err)
			}
			kvRecord[colName] = converted
		} else {
			kvRecord[colName] = nil
		}
	}

	// Serialize to JSON
	value, err := json.Marshal(kvRecord)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal record to JSON: %w", err)
	}

	return key, value, nil
}

// FromKV deserializes a key-value pair back into a relational record.
// The schema is used to properly reconstruct the record structure.
func (t *Translator) FromKV(key string, value []byte, schema *core.Schema) (map[string]interface{}, error) {
	if value == nil {
		return nil, fmt.Errorf("value cannot be nil")
	}

	if schema == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}

	// Deserialize JSON
	var kvRecord map[string]interface{}
	if err := json.Unmarshal(value, &kvRecord); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Convert values from DB types back to Go types
	record := make(map[string]interface{}, len(kvRecord))
	for colName, value := range kvRecord {
		// Find the column definition to get the DB type
		var dbType string
		for _, col := range schema.Columns {
			if col.Name == colName {
				dbType = col.Type
				break
			}
		}

		// Convert value from DB type to Go type
		if value != nil {
			converted, err := t.mapper.ConvertFromDBValue(value, dbType)
			if err != nil {
				return nil, fmt.Errorf("failed to convert value for column '%s': %w", colName, err)
			}
			record[colName] = converted
		} else {
			record[colName] = nil
		}
	}

	// Validate the reconstructed record
	validator := NewSchemaValidator(schema)
	if err := validator.ValidateRecord(record); err != nil {
		// Log warning but don't fail - the data might be valid but schema might have changed
		// In production, you might want to handle schema evolution here
	}

	return record, nil
}

// ToDB converts a record map into a SQL statement and arguments.
// Returns the SQL query string and the arguments for parameterized queries.
// This generates an INSERT statement with all columns.
func (t *Translator) ToDB(record map[string]interface{}, schema *core.Schema) (string, []interface{}, error) {
	if record == nil {
		return "", nil, fmt.Errorf("record cannot be nil")
	}

	if schema == nil {
		return "", nil, fmt.Errorf("schema cannot be nil")
	}

	// Validate the record
	validator := NewSchemaValidator(schema)
	if err := validator.ValidateRecord(record); err != nil {
		return "", nil, fmt.Errorf("validation failed: %w", err)
	}

	// Build column list and values
	columns := make([]string, 0, len(schema.Columns))
	placeholders := make([]string, 0, len(schema.Columns))
	args := make([]interface{}, 0, len(schema.Columns))

	for _, col := range schema.Columns {
		value, exists := record[col.Name]

		// Skip NULL values if column is nullable and value doesn't exist
		// For required columns, validation should have caught this
		if !exists && col.Nullable {
			continue
		}

		columns = append(columns, col.Name)
		placeholders = append(placeholders, "?")

		// Convert value to DB-compatible type
		if value != nil {
			converted, err := t.mapper.ConvertToDBValue(value, col.Type)
			if err != nil {
				return "", nil, fmt.Errorf("failed to convert value for column '%s': %w", col.Name, err)
			}
			args = append(args, converted)
		} else {
			args = append(args, nil)
		}
	}

	if len(columns) == 0 {
		return "", nil, fmt.Errorf("no columns to insert")
	}

	// Build INSERT statement
	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		schema.TableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return query, args, nil
}

// ToDBUpdate converts a record map into an UPDATE SQL statement and arguments.
// This is a helper method for UPDATE operations.
func (t *Translator) ToDBUpdate(key interface{}, updates map[string]interface{}, schema *core.Schema) (string, []interface{}, error) {
	if updates == nil || len(updates) == 0 {
		return "", nil, fmt.Errorf("updates cannot be empty")
	}

	if schema == nil {
		return "", nil, fmt.Errorf("schema cannot be nil")
	}

	// Validate partial record
	validator := NewSchemaValidator(schema)
	if err := validator.ValidatePartialRecord(updates); err != nil {
		return "", nil, fmt.Errorf("validation failed: %w", err)
	}

	// Validate primary key
	if err := validator.ValidatePrimaryKey(key); err != nil {
		return "", nil, fmt.Errorf("invalid primary key: %w", err)
	}

	// Ensure we're not trying to update the primary key
	if _, exists := updates[schema.PrimaryKey]; exists {
		return "", nil, fmt.Errorf("cannot update primary key '%s'", schema.PrimaryKey)
	}

	// Build SET clause
	setParts := make([]string, 0, len(updates))
	args := make([]interface{}, 0, len(updates)+1)

	for colName, value := range updates {
		// Find the column definition
		var col *core.Column
		for i := range schema.Columns {
			if schema.Columns[i].Name == colName {
				col = &schema.Columns[i]
				break
			}
		}

		if col == nil {
			// Column not in schema - might be a new field, include it anyway
			setParts = append(setParts, fmt.Sprintf("%s = ?", colName))
			args = append(args, value)
			continue
		}

		setParts = append(setParts, fmt.Sprintf("%s = ?", colName))

		// Convert value to DB-compatible type
		if value != nil {
			converted, err := t.mapper.ConvertToDBValue(value, col.Type)
			if err != nil {
				return "", nil, fmt.Errorf("failed to convert value for column '%s': %w", colName, err)
			}
			args = append(args, converted)
		} else {
			args = append(args, nil)
		}
	}

	if len(setParts) == 0 {
		return "", nil, fmt.Errorf("no columns to update")
	}

	// Build UPDATE statement
	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = ?",
		schema.TableName,
		strings.Join(setParts, ", "),
		schema.PrimaryKey,
	)

	// Add primary key value to args
	pkValue, err := t.mapper.ConvertToDBValue(key, schema.Columns[0].Type) // Use first column type as fallback
	// Actually, find the primary key column type
	for _, col := range schema.Columns {
		if col.Name == schema.PrimaryKey {
			pkValue, err = t.mapper.ConvertToDBValue(key, col.Type)
			break
		}
	}
	if err != nil {
		return "", nil, fmt.Errorf("failed to convert primary key value: %w", err)
	}
	args = append(args, pkValue)

	return query, args, nil
}

// FromDB converts a database row into a record map.
// The schema is used to properly map column types and handle NULL values.
func (t *Translator) FromDB(row core.Row, schema *core.Schema) (map[string]interface{}, error) {
	if row == nil {
		return nil, fmt.Errorf("row cannot be nil")
	}

	if schema == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}

	// Create a slice of interface{} pointers for scanning
	// We need to use pointers so Scan can populate them
	values := make([]interface{}, len(schema.Columns))
	valuePtrs := make([]interface{}, len(schema.Columns))

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	// Scan the row
	if err := row.Scan(valuePtrs...); err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Convert scanned values to record map
	record := make(map[string]interface{}, len(schema.Columns))
	for i, col := range schema.Columns {
		value := values[i]

		// Convert from DB type to Go type
		if value != nil {
			converted, err := t.mapper.ConvertFromDBValue(value, col.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to convert value for column '%s': %w", col.Name, err)
			}
			record[col.Name] = converted
		} else {
			record[col.Name] = nil
		}
	}

	return record, nil
}
