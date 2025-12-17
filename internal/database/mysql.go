package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/rzpsarthak13/shock-absorber/internal/core"
)

// MySQLDatabase implements the core.Database interface using MySQL.
type MySQLDatabase struct {
	db     *sql.DB
	closed bool
}

// NewMySQLDatabase creates a new MySQL database implementation.
func NewMySQLDatabase(host string, port int, database, username, password string, maxOpenConns, maxIdleConns int, connMaxLifetime, connMaxIdleTime, connectionTimeout time.Duration) (*MySQLDatabase, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=%s",
		username, password, host, port, database, connectionTimeout)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &MySQLDatabase{
		db:     db,
		closed: false,
	}, nil
}

// Query executes a SELECT query and returns rows.
func (m *MySQLDatabase) Query(ctx context.Context, query string, args ...interface{}) (core.Rows, error) {
	if m.closed {
		return nil, fmt.Errorf("database is closed")
	}
	log.Printf("[MYSQL] Executing query: %s with args: %v", query, args)
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("[MYSQL] ERROR: Query failed: %v", err)
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	log.Printf("[MYSQL] Query executed successfully")
	return &mysqlRows{rows: rows}, nil
}

// Exec executes a non-query statement and returns a result.
func (m *MySQLDatabase) Exec(ctx context.Context, query string, args ...interface{}) (core.Result, error) {
	if m.closed {
		return nil, fmt.Errorf("database is closed")
	}
	log.Printf("[MYSQL] Executing statement: %s with args: %v", query, args)
	result, err := m.db.ExecContext(ctx, query, args...)
	if err != nil {
		log.Printf("[MYSQL] ERROR: Exec failed: %v", err)
		return nil, fmt.Errorf("failed to execute statement: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("[MYSQL] Statement executed successfully (rows affected: %d)", rowsAffected)
	return &mysqlResult{result: result}, nil
}

// BeginTx starts a new transaction.
func (m *MySQLDatabase) BeginTx(ctx context.Context) (core.Transaction, error) {
	if m.closed {
		return nil, fmt.Errorf("database is closed")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	return &mysqlTransaction{tx: tx}, nil
}

// GetSchema retrieves the schema information for a specific table.
func (m *MySQLDatabase) GetSchema(ctx context.Context, tableName string) (*core.Schema, error) {
	if m.closed {
		return nil, fmt.Errorf("database is closed")
	}

	schema := &core.Schema{
		TableName: tableName,
		Columns:   []core.Column{},
		Indexes:   []core.Index{},
	}

	// Get columns
	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`
	rows, err := m.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var colName, dataType, isNullable, columnKey string
		var colDefault sql.NullString
		if err := rows.Scan(&colName, &dataType, &isNullable, &colDefault, &columnKey); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		column := core.Column{
			Name:     colName,
			Type:     dataType,
			Nullable: isNullable == "YES",
			Default:  nil,
		}
		if colDefault.Valid {
			column.Default = colDefault.String
		}

		// Check if this is the primary key
		if columnKey == "PRI" {
			schema.PrimaryKey = colName
		}

		schema.Columns = append(schema.Columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating columns: %w", err)
	}

	if schema.PrimaryKey == "" {
		return nil, fmt.Errorf("table %s does not have a primary key", tableName)
	}

	// Get indexes
	indexQuery := `
		SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE
		FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX
	`
	indexRows, err := m.db.QueryContext(ctx, indexQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexes: %w", err)
	}
	defer indexRows.Close()

	indexMap := make(map[string]*core.Index)
	for indexRows.Next() {
		var indexName, columnName string
		var nonUnique int
		if err := indexRows.Scan(&indexName, &columnName, &nonUnique); err != nil {
			return nil, fmt.Errorf("failed to scan index: %w", err)
		}

		if index, exists := indexMap[indexName]; exists {
			index.Columns = append(index.Columns, columnName)
		} else {
			isPrimary := indexName == "PRIMARY"
			indexMap[indexName] = &core.Index{
				Name:    indexName,
				Columns: []string{columnName},
				Unique:  nonUnique == 0,
				Primary: isPrimary,
			}
		}
	}

	for _, index := range indexMap {
		schema.Indexes = append(schema.Indexes, *index)
	}

	return schema, nil
}

// GetTables returns a list of all table names in the database.
func (m *MySQLDatabase) GetTables(ctx context.Context) ([]string, error) {
	if m.closed {
		return nil, fmt.Errorf("database is closed")
	}

	query := `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'
	`
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	return tables, rows.Err()
}

// Close closes the database connection.
func (m *MySQLDatabase) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	return m.db.Close()
}

// mysqlRows wraps sql.Rows to implement core.Rows.
type mysqlRows struct {
	rows *sql.Rows
}

func (r *mysqlRows) Next() bool {
	return r.rows.Next()
}

func (r *mysqlRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *mysqlRows) Close() error {
	return r.rows.Close()
}

func (r *mysqlRows) Err() error {
	return r.rows.Err()
}

// mysqlResult wraps sql.Result to implement core.Result.
type mysqlResult struct {
	result sql.Result
}

func (r *mysqlResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

func (r *mysqlResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

// mysqlTransaction wraps sql.Tx to implement core.Transaction.
type mysqlTransaction struct {
	tx *sql.Tx
}

func (t *mysqlTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *mysqlTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *mysqlTransaction) Query(query string, args ...interface{}) (core.Rows, error) {
	rows, err := t.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return &mysqlRows{rows: rows}, nil
}

func (t *mysqlTransaction) Exec(query string, args ...interface{}) (core.Result, error) {
	result, err := t.tx.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	return &mysqlResult{result: result}, nil
}
