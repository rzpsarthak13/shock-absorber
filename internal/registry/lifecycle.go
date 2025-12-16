package registry

import (
	"context"
	"sync"

	"github.com/razorpay/shock-absorber/internal/core"
)

// LifecycleHook defines a hook that can be executed during table lifecycle events.
// Hooks are called synchronously during enable/disable operations.
type LifecycleHook interface {
	// OnEnable is called when a table is being enabled for KV operations.
	// If this hook returns an error, the enable operation will fail.
	// The context can be used for cancellation and timeout handling.
	OnEnable(ctx context.Context, tableName string, schema *core.Schema) error

	// OnDisable is called when a table is being disabled for KV operations.
	// If this hook returns an error, the disable operation will fail.
	// The context can be used for cancellation and timeout handling.
	OnDisable(ctx context.Context, tableName string, schema *core.Schema) error
}

// LifecycleHookFunc is a function type that implements LifecycleHook.
// This allows simple functions to be used as hooks without implementing the interface.
type LifecycleHookFunc struct {
	OnEnableFunc  func(ctx context.Context, tableName string, schema *core.Schema) error
	OnDisableFunc func(ctx context.Context, tableName string, schema *core.Schema) error
}

// OnEnable calls the OnEnableFunc if it's not nil.
func (f LifecycleHookFunc) OnEnable(ctx context.Context, tableName string, schema *core.Schema) error {
	if f.OnEnableFunc != nil {
		return f.OnEnableFunc(ctx, tableName, schema)
	}
	return nil
}

// OnDisable calls the OnDisableFunc if it's not nil.
func (f LifecycleHookFunc) OnDisable(ctx context.Context, tableName string, schema *core.Schema) error {
	if f.OnDisableFunc != nil {
		return f.OnDisableFunc(ctx, tableName, schema)
	}
	return nil
}

// LifecycleManager manages lifecycle hooks for tables.
// It allows registering multiple hooks that will be executed during enable/disable operations.
type LifecycleManager struct {
	mu    sync.RWMutex
	hooks []LifecycleHook
}

// NewLifecycleManager creates a new lifecycle manager.
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		hooks: make([]LifecycleHook, 0),
	}
}

// RegisterHook registers a lifecycle hook that will be called during table enable/disable operations.
// Hooks are executed in the order they were registered.
func (lm *LifecycleManager) RegisterHook(hook LifecycleHook) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.hooks = append(lm.hooks, hook)
}

// UnregisterHook removes a hook from the manager.
// This is useful for cleanup or dynamic hook management.
func (lm *LifecycleManager) UnregisterHook(hook LifecycleHook) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	for i, h := range lm.hooks {
		if h == hook {
			lm.hooks = append(lm.hooks[:i], lm.hooks[i+1:]...)
			return
		}
	}
}

// ExecuteEnableHooks executes all registered enable hooks in order.
// If any hook returns an error, execution stops and the error is returned.
func (lm *LifecycleManager) ExecuteEnableHooks(ctx context.Context, tableName string, schema *core.Schema) error {
	lm.mu.RLock()
	hooks := make([]LifecycleHook, len(lm.hooks))
	copy(hooks, lm.hooks)
	lm.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook.OnEnable(ctx, tableName, schema); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteDisableHooks executes all registered disable hooks in order.
// If any hook returns an error, execution stops and the error is returned.
func (lm *LifecycleManager) ExecuteDisableHooks(ctx context.Context, tableName string, schema *core.Schema) error {
	lm.mu.RLock()
	hooks := make([]LifecycleHook, len(lm.hooks))
	copy(hooks, lm.hooks)
	lm.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook.OnDisable(ctx, tableName, schema); err != nil {
			return err
		}
	}
	return nil
}

// ClearHooks removes all registered hooks.
func (lm *LifecycleManager) ClearHooks() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.hooks = make([]LifecycleHook, 0)
}

// HookCount returns the number of registered hooks.
func (lm *LifecycleManager) HookCount() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return len(lm.hooks)
}
