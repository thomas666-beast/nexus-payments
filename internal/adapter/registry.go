package adapter

import (
	"fmt"
	"sync"
)

// Registry maps bank IDs to adapters. Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds an adapter. Panics if the bank ID is already registered —
// silent overwrite is a configuration bug, not a runtime condition.
func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := a.BankID()
	if id == "" {
		panic("adapter registered with empty bank id")
	}
	if _, exists := r.adapters[id]; exists {
		panic(fmt.Sprintf("adapter for bank %q already registered", id))
	}
	r.adapters[id] = a
}

// Get returns the adapter for a bank. Returns ErrUnknownBank if absent.
func (r *Registry) Get(bankID string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[bankID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownBank, bankID)
	}
	return a, nil
}

// List returns all registered bank IDs. Useful for diagnostics.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.adapters))
	for id := range r.adapters {
		ids = append(ids, id)
	}
	return ids
}
