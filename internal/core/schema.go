package core

// Schema represents the structure of a database table.
type Schema struct {
	// TableName is the name of the table.
	TableName string

	// PrimaryKey is the name of the primary key column.
	PrimaryKey string

	// Columns contains all column definitions for the table.
	Columns []Column

	// Indexes contains all index definitions for the table.
	Indexes []Index
}

// Column represents a single column in a database table.
type Column struct {
	// Name is the column name.
	Name string

	// Type is the database type (e.g., "INT", "VARCHAR(255)", "TIMESTAMP").
	Type string

	// Nullable indicates whether the column can contain NULL values.
	Nullable bool

	// Default is the default value for the column, if any.
	Default interface{}
}

// Index represents a database index.
type Index struct {
	// Name is the index name.
	Name string

	// Columns are the column names that make up this index.
	Columns []string

	// Unique indicates whether this is a unique index.
	Unique bool

	// Primary indicates whether this is the primary key index.
	Primary bool
}

// SchemaTranslator defines the interface for translating between
// relational database records and key-value store formats.
type SchemaTranslator interface {
	// ToKV converts a relational record to a key-value pair.
	// Returns the key (typically table:primary_key_value) and the serialized value.
	ToKV(record map[string]interface{}, schema *Schema) (string, []byte, error)

	// FromKV deserializes a key-value pair back into a relational record.
	// The schema is used to properly reconstruct the record structure.
	FromKV(key string, value []byte, schema *Schema) (map[string]interface{}, error)

	// ToDB converts a record map into a SQL statement and arguments.
	// Returns the SQL query string and the arguments for parameterized queries.
	ToDB(record map[string]interface{}, schema *Schema) (string, []interface{}, error)

	// FromDB converts a database row into a record map.
	// The schema is used to properly map column types and handle NULL values.
	FromDB(row Row, schema *Schema) (map[string]interface{}, error)
}

