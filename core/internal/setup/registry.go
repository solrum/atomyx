package setup

import "fmt"

// Entry is one registered step and the steps it depends on.
type Entry struct {
	Step Step
	// Requires lists the IDs of steps that must be satisfied before
	// this one runs. When one is not, the runner skips this step.
	Requires []string
}

// Registry holds steps in the order they run. A step may require only
// steps registered before it, so the order is always a valid
// execution order and the dependency graph cannot contain a cycle.
type Registry struct {
	entries []Entry
	ids     map[string]bool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{ids: map[string]bool{}}
}

// Add appends step after every step already registered. It fails when
// the ID is empty or already registered, or when a required ID is not
// registered yet.
func (r *Registry) Add(step Step, requires ...string) error {
	id := step.ID()
	if id == "" {
		return fmt.Errorf("setup step has an empty id")
	}
	if r.ids[id] {
		return fmt.Errorf("setup step %q is registered twice", id)
	}
	for _, req := range requires {
		if !r.ids[req] {
			return fmt.Errorf("setup step %q requires %q, which is not registered before it", id, req)
		}
	}
	r.ids[id] = true
	r.entries = append(r.entries, Entry{Step: step, Requires: append([]string(nil), requires...)})
	return nil
}

// Entries returns the registered entries in run order.
func (r *Registry) Entries() []Entry {
	return append([]Entry(nil), r.entries...)
}
