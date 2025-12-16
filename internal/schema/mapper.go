package schema

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// TypeMapper handles mapping between database types and Go types.
type TypeMapper struct{}

// NewTypeMapper creates a new type mapper.
func NewTypeMapper() *TypeMapper {
	return &TypeMapper{}
}

// MapDBTypeToGoType converts a database type string to a Go type.
// This helps in understanding what Go type to expect for a given DB column type.
func (tm *TypeMapper) MapDBTypeToGoType(dbType string) reflect.Type {
	dbTypeUpper := strings.ToUpper(strings.TrimSpace(dbType))

	// Remove size/precision information (e.g., VARCHAR(255) -> VARCHAR)
	baseType := dbTypeUpper
	if idx := strings.Index(dbTypeUpper, "("); idx > 0 {
		baseType = dbTypeUpper[:idx]
	}

	switch baseType {
	case "INT", "INTEGER", "MEDIUMINT":
		return reflect.TypeOf(int(0))
	case "BIGINT":
		return reflect.TypeOf(int64(0))
	case "SMALLINT", "TINYINT":
		return reflect.TypeOf(int16(0))
	case "FLOAT":
		return reflect.TypeOf(float32(0))
	case "DOUBLE", "DOUBLE PRECISION", "REAL":
		return reflect.TypeOf(float64(0))
	case "DECIMAL", "NUMERIC":
		return reflect.TypeOf("") // Store as string to preserve precision
	case "VARCHAR", "CHAR", "TEXT", "LONGTEXT", "MEDIUMTEXT", "TINYTEXT":
		return reflect.TypeOf("")
	case "BINARY", "VARBINARY", "BLOB", "LONGBLOB", "MEDIUMBLOB", "TINYBLOB":
		return reflect.TypeOf([]byte{})
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return reflect.TypeOf(time.Time{})
	case "BOOLEAN", "BOOL", "TINYINT(1)":
		return reflect.TypeOf(false)
	case "JSON", "JSONB":
		return reflect.TypeOf(map[string]interface{}(nil))
	default:
		// Default to string for unknown types
		return reflect.TypeOf("")
	}
}

// ConvertToDBValue converts a Go value to a database-compatible value.
// Handles type conversions and NULL values appropriately.
func (tm *TypeMapper) ConvertToDBValue(value interface{}, dbType string) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	dbTypeUpper := strings.ToUpper(strings.TrimSpace(dbType))
	baseType := dbTypeUpper
	if idx := strings.Index(dbTypeUpper, "("); idx > 0 {
		baseType = dbTypeUpper[:idx]
	}

	switch baseType {
	case "INT", "INTEGER", "MEDIUMINT":
		return tm.toInt(value)
	case "BIGINT":
		return tm.toInt64(value)
	case "SMALLINT", "TINYINT":
		return tm.toInt16(value)
	case "FLOAT":
		return tm.toFloat32(value)
	case "DOUBLE", "DOUBLE PRECISION", "REAL":
		return tm.toFloat64(value)
	case "DECIMAL", "NUMERIC":
		return tm.toString(value)
	case "VARCHAR", "CHAR", "TEXT", "LONGTEXT", "MEDIUMTEXT", "TINYTEXT":
		return tm.toString(value)
	case "BINARY", "VARBINARY", "BLOB", "LONGBLOB", "MEDIUMBLOB", "TINYBLOB":
		return tm.toBytes(value)
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return tm.toTime(value)
	case "BOOLEAN", "BOOL", "TINYINT(1)":
		return tm.toBool(value)
	case "JSON", "JSONB":
		return tm.toJSON(value)
	default:
		// For unknown types, try to convert to string
		return tm.toString(value)
	}
}

// ConvertFromDBValue converts a database value to a Go value.
// Handles NULL values and type conversions.
func (tm *TypeMapper) ConvertFromDBValue(value interface{}, dbType string) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	// Handle sql.Null* types
	if valuer, ok := value.(driver.Valuer); ok {
		val, err := valuer.Value()
		if err != nil {
			return nil, err
		}
		if val == nil {
			return nil, nil
		}
		value = val
	}

	dbTypeUpper := strings.ToUpper(strings.TrimSpace(dbType))
	baseType := dbTypeUpper
	if idx := strings.Index(dbTypeUpper, "("); idx > 0 {
		baseType = dbTypeUpper[:idx]
	}

	switch baseType {
	case "INT", "INTEGER", "MEDIUMINT":
		return tm.toInt(value)
	case "BIGINT":
		return tm.toInt64(value)
	case "SMALLINT", "TINYINT":
		return tm.toInt16(value)
	case "FLOAT":
		return tm.toFloat32(value)
	case "DOUBLE", "DOUBLE PRECISION", "REAL":
		return tm.toFloat64(value)
	case "DECIMAL", "NUMERIC":
		return tm.toString(value)
	case "VARCHAR", "CHAR", "TEXT", "LONGTEXT", "MEDIUMTEXT", "TINYTEXT":
		return tm.toString(value)
	case "BINARY", "VARBINARY", "BLOB", "LONGBLOB", "MEDIUMBLOB", "TINYBLOB":
		return tm.toBytes(value)
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return tm.toTime(value)
	case "BOOLEAN", "BOOL", "TINYINT(1)":
		return tm.toBool(value)
	case "JSON", "JSONB":
		return tm.toJSON(value)
	default:
		// For unknown types, return as-is
		return value, nil
	}
}

// Helper conversion functions

func (tm *TypeMapper) toInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to int: %w", err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", value)
	}
}

func (tm *TypeMapper) toInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to int64: %w", err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", value)
	}
}

func (tm *TypeMapper) toInt16(value interface{}) (int16, error) {
	switch v := value.(type) {
	case int16:
		return v, nil
	case int:
		return int16(v), nil
	case int8:
		return int16(v), nil
	case int32:
		return int16(v), nil
	case int64:
		return int16(v), nil
	case uint:
		return int16(v), nil
	case uint8:
		return int16(v), nil
	case uint16:
		return int16(v), nil
	case float32:
		return int16(v), nil
	case float64:
		return int16(v), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to int16: %w", err)
		}
		return int16(i), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int16", value)
	}
}

func (tm *TypeMapper) toFloat32(value interface{}) (float32, error) {
	switch v := value.(type) {
	case float32:
		return v, nil
	case float64:
		return float32(v), nil
	case int:
		return float32(v), nil
	case int64:
		return float32(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 32)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to float32: %w", err)
		}
		return float32(f), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float32", value)
	}
}

func (tm *TypeMapper) toFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to float64: %w", err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

func (tm *TypeMapper) toString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case time.Time:
		return v.Format(time.RFC3339), nil
	default:
		// Try JSON encoding for complex types
		bytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("cannot convert %T to string: %w", value, err)
		}
		return string(bytes), nil
	}
}

func (tm *TypeMapper) toBytes(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to []byte", value)
	}
}

func (tm *TypeMapper) toTime(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		// Try common time formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse time string: %s", v)
	case int64:
		// Assume Unix timestamp
		return time.Unix(v, 0), nil
	default:
		return time.Time{}, fmt.Errorf("cannot convert %T to time.Time", value)
	}
}

func (tm *TypeMapper) toBool(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case int, int8, int16, int32, int64:
		// Non-zero is true
		return reflect.ValueOf(v).Int() != 0, nil
	case uint, uint8, uint16, uint32, uint64:
		// Non-zero is true
		return reflect.ValueOf(v).Uint() != 0, nil
	case string:
		b, err := strconv.ParseBool(v)
		if err != nil {
			// Try numeric string
			if i, err := strconv.ParseInt(v, 10, 64); err == nil {
				return i != 0, nil
			}
			return false, fmt.Errorf("cannot convert string to bool: %w", err)
		}
		return b, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

func (tm *TypeMapper) toJSON(value interface{}) (interface{}, error) {
	// For MySQL JSON columns, we need to return a JSON string, not a map/slice
	// MySQL driver expects JSON columns to be strings containing valid JSON
	switch v := value.(type) {
	case string:
		// If it's already a string, validate it's valid JSON
		var result interface{}
		if err := json.Unmarshal([]byte(v), &result); err != nil {
			return nil, fmt.Errorf("cannot parse JSON string: %w", err)
		}
		// Return as-is if it's already a valid JSON string
		return v, nil
	case []byte:
		// If it's already bytes, validate and return as string
		var result interface{}
		if err := json.Unmarshal(v, &result); err != nil {
			return nil, fmt.Errorf("cannot parse JSON bytes: %w", err)
		}
		return string(v), nil
	case map[string]interface{}:
		// Marshal maps to JSON string for MySQL
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal map to JSON: %w", err)
		}
		return string(jsonBytes), nil
	case []interface{}:
		// Marshal slices to JSON string for MySQL
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal slice to JSON: %w", err)
		}
		return string(jsonBytes), nil
	default:
		// For other types, try to marshal to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot marshal %T to JSON: %w", v, err)
		}
		return string(jsonBytes), nil
	}
}
