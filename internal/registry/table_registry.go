package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/razorpay/shock-absorber/internal/core"
)

// TableMetadata contains metadata about a registered table.
type TableMetadata struct {
	// TableName is the name of the table.
	TableName string

	// Table is the table implementation instance.
	Table core.Table

	// Schema contains the database schema information for this table.
	Schema *core.Schema

	// Config contains the table-specific configuration.
	Config InternalTableConfig

	// Enabled indicates whether KV operations are currently enabled for this table.
	Enabled bool

	// EnabledAt is the timestamp when the table was last enabled.
	EnabledAt *time.Time

	// DisabledAt is the timestamp when the table was last disabled.
	DisabledAt *time.Time

	// CreatedAt is the timestamp when the table was registered.
	CreatedAt time.Time

	// UpdatedAt is the timestamp when the table metadata was last updated.
	UpdatedAt time.Time
}

// TableRegistry manages enabled tables and their lifecycle.
// It provides thread-safe operations for registering, enabling, disabling, and retrieving tables.
type TableRegistry struct {
	mu        sync.RWMutex
	tables    map[string]*TableMetadata
	configMgr *ConfigManager
	lifecycle *LifecycleManager
}

// NewTableRegistry creates a new table registry with the given configuration manager and lifecycle manager.
func NewTableRegistry(configMgr *ConfigManager, lifecycle *LifecycleManager) *TableRegistry {
	if lifecycle == nil {
		lifecycle = NewLifecycleManager()
	}
	return &TableRegistry{
		tables:    make(map[string]*TableMetadata),
		configMgr: configMgr,
		lifecycle: lifecycle,
	}
}

// Register registers a table in the registry.
// The table must have a valid schema. If the table already exists, it will be updated.
// This does not enable KV operations - use EnableKV() for that.
func (tr *TableRegistry) Register(ctx context.Context, tableName string, table core.Table, schema *core.Schema) error {
	if tableName == "" {
		return fmt.Errorf("table name cannot be empty")
	}
	if table == nil {
		return fmt.Errorf("table cannot be nil")
	}
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}
	if schema.TableName != tableName {
		return fmt.Errorf("schema table name %q does not match provided table name %q", schema.TableName, tableName)
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	now := time.Now()
	config := tr.configMgr.GetTableConfig(tableName)

	metadata := &TableMetadata{
		TableName: tableName,
		Table:     table,
		Schema:    schema,
		Config:    config,
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// If table already exists, preserve enabled state and timestamps
	if existing, exists := tr.tables[tableName]; exists {
		metadata.Enabled = existing.Enabled
		metadata.EnabledAt = existing.EnabledAt
		metadata.DisabledAt = existing.DisabledAt
		metadata.CreatedAt = existing.CreatedAt
	}

	tr.tables[tableName] = metadata
	return nil
}

// Get retrieves a table by name. Returns nil if the table is not registered.
func (tr *TableRegistry) Get(tableName string) (core.Table, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return nil, fmt.Errorf("table %q is not registered", tableName)
	}

	return metadata.Table, nil
}

// GetMetadata retrieves the metadata for a table by name.
// Returns nil if the table is not registered.
func (tr *TableRegistry) GetMetadata(tableName string) (*TableMetadata, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return nil, fmt.Errorf("table %q is not registered", tableName)
	}

	// Return a copy to prevent external modifications
	return &TableMetadata{
		TableName:  metadata.TableName,
		Table:      metadata.Table,
		Schema:     metadata.Schema,
		Config:     metadata.Config,
		Enabled:    metadata.Enabled,
		EnabledAt:  metadata.EnabledAt,
		DisabledAt: metadata.DisabledAt,
		CreatedAt:  metadata.CreatedAt,
		UpdatedAt:  metadata.UpdatedAt,
	}, nil
}

// EnableKV enables KV store operations for a table.
// This will:
// 1. Execute all registered enable hooks
// 2. Call the table's EnableKV() method
// 3. Update the table metadata
// If any step fails, the operation is rolled back.
func (tr *TableRegistry) EnableKV(ctx context.Context, tableName string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return fmt.Errorf("table %q is not registered", tableName)
	}

	if metadata.Enabled {
		return nil // Already enabled
	}

	// Execute lifecycle hooks before enabling
	if err := tr.lifecycle.ExecuteEnableHooks(ctx, tableName, metadata.Schema); err != nil {
		return fmt.Errorf("enable hook failed for table %q: %w", tableName, err)
	}

	// Call the table's EnableKV method
	if err := metadata.Table.EnableKV(); err != nil {
		return fmt.Errorf("failed to enable KV for table %q: %w", tableName, err)
	}

	// Update metadata
	now := time.Now()
	metadata.Enabled = true
	metadata.EnabledAt = &now
	metadata.DisabledAt = nil
	metadata.UpdatedAt = now

	return nil
}

// DisableKV disables KV store operations for a table.
// This will:
// 1. Execute all registered disable hooks
// 2. Call the table's DisableKV() method
// 3. Update the table metadata
// If any step fails, the operation is rolled back.
func (tr *TableRegistry) DisableKV(ctx context.Context, tableName string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return fmt.Errorf("table %q is not registered", tableName)
	}

	if !metadata.Enabled {
		return nil // Already disabled
	}

	// Execute lifecycle hooks before disabling
	if err := tr.lifecycle.ExecuteDisableHooks(ctx, tableName, metadata.Schema); err != nil {
		return fmt.Errorf("disable hook failed for table %q: %w", tableName, err)
	}

	// Call the table's DisableKV method
	if err := metadata.Table.DisableKV(); err != nil {
		return fmt.Errorf("failed to disable KV for table %q: %w", tableName, err)
	}

	// Update metadata
	now := time.Now()
	metadata.Enabled = false
	metadata.DisabledAt = &now
	metadata.EnabledAt = nil
	metadata.UpdatedAt = now

	return nil
}

// Unregister removes a table from the registry.
// If the table is currently enabled, it will be disabled first.
func (tr *TableRegistry) Unregister(ctx context.Context, tableName string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return fmt.Errorf("table %q is not registered", tableName)
	}

	// Disable if currently enabled
	if metadata.Enabled {
		// Execute disable hooks
		if err := tr.lifecycle.ExecuteDisableHooks(ctx, tableName, metadata.Schema); err != nil {
			return fmt.Errorf("disable hook failed during unregister for table %q: %w", tableName, err)
		}

		// Call the table's DisableKV method
		if err := metadata.Table.DisableKV(); err != nil {
			return fmt.Errorf("failed to disable KV during unregister for table %q: %w", tableName, err)
		}
	}

	delete(tr.tables, tableName)
	return nil
}

// List returns a list of all registered table names.
func (tr *TableRegistry) List() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	names := make([]string, 0, len(tr.tables))
	for name := range tr.tables {
		names = append(names, name)
	}
	return names
}

// ListEnabled returns a list of all enabled table names.
func (tr *TableRegistry) ListEnabled() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	names := make([]string, 0)
	for name, metadata := range tr.tables {
		if metadata.Enabled {
			names = append(names, name)
		}
	}
	return names
}

// ListDisabled returns a list of all disabled table names.
func (tr *TableRegistry) ListDisabled() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	names := make([]string, 0)
	for name, metadata := range tr.tables {
		if !metadata.Enabled {
			names = append(names, name)
		}
	}
	return names
}

// IsEnabled checks if a table is currently enabled for KV operations.
func (tr *TableRegistry) IsEnabled(tableName string) (bool, error) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return false, fmt.Errorf("table %q is not registered", tableName)
	}

	return metadata.Enabled, nil
}

// UpdateConfig updates the configuration for a registered table.
// The configuration is merged with defaults from the config manager.
func (tr *TableRegistry) UpdateConfig(tableName string) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	metadata, exists := tr.tables[tableName]
	if !exists {
		return fmt.Errorf("table %q is not registered", tableName)
	}

	metadata.Config = tr.configMgr.GetTableConfig(tableName)
	metadata.UpdatedAt = time.Now()
	return nil
}

// RefreshConfig refreshes the configuration for all registered tables.
// This is useful when the global configuration changes.
func (tr *TableRegistry) RefreshConfig() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	now := time.Now()
	for _, metadata := range tr.tables {
		metadata.Config = tr.configMgr.GetTableConfig(metadata.TableName)
		metadata.UpdatedAt = now
	}
}

// GetLifecycleManager returns the lifecycle manager associated with this registry.
func (tr *TableRegistry) GetLifecycleManager() *LifecycleManager {
	return tr.lifecycle
}

// Count returns the total number of registered tables.
func (tr *TableRegistry) Count() int {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return len(tr.tables)
}

// Clear removes all tables from the registry.
// All tables will be disabled before removal.
func (tr *TableRegistry) Clear(ctx context.Context) error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Disable all enabled tables
	for tableName, metadata := range tr.tables {
		if metadata.Enabled {
			// Execute disable hooks
			if err := tr.lifecycle.ExecuteDisableHooks(ctx, tableName, metadata.Schema); err != nil {
				return fmt.Errorf("disable hook failed during clear for table %q: %w", tableName, err)
			}

			// Call the table's DisableKV method
			if err := metadata.Table.DisableKV(); err != nil {
				return fmt.Errorf("failed to disable KV during clear for table %q: %w", tableName, err)
			}
		}
	}

	tr.tables = make(map[string]*TableMetadata)
	return nil
}
