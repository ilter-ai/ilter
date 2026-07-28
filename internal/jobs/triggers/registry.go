package triggers

import (
	"fmt"
	"sync"
)

// Registry is a goroutine-safe registry of Trigger instances keyed by ID.
// Uses sync.RWMutex for concurrent read access (same pattern as auth cache
// and load balancer in this codebase).
type Registry struct {
	mu    sync.RWMutex
	items map[string]Trigger
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		items: make(map[string]Trigger),
	}
}

// Register adds a trigger to the catalog. Returns an error if a trigger
// with the same ID already exists.
func (r *Registry) Register(t Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[t.ID()]; exists {
		return fmt.Errorf("trigger %s already registered", t.ID())
	}
	r.items[t.ID()] = t
	return nil
}

// Get retrieves a trigger by ID. Returns (trigger, true) if found,
// (nil, false) otherwise.
func (r *Registry) Get(id string) (Trigger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[id]
	return t, ok
}

// GetByJobID returns all triggers associated with the given job ID.
func (r *Registry) GetByJobID(jobID string) []Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Trigger
	for _, t := range r.items {
		if t.JobID() == jobID {
			result = append(result, t)
		}
	}
	return result
}

// Delete removes a trigger from the registry by ID. No-op if the ID
// does not exist.
func (r *Registry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
}

// List returns a copy of all triggers in the catalog.
func (r *Registry) List() []Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Trigger, 0, len(r.items))
	for _, t := range r.items {
		result = append(result, t)
	}
	return result
}

// Len returns the number of triggers in the catalog.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}
