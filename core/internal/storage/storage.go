package storage

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// Storage is persistent record storage for framework data that must
// outlive the process: bug reports, case studies, recorded test
// runs. Each record is identified by a string key. Namespacing is by
// key prefix, not directories. Callers append their own prefix to
// avoid collision ("bugs/123", "case-studies/2026-04/x", "runs/abc").
type Storage interface {
	// Save persists data under key. A string value is written as raw
	// text (case-study bodies are markdown); any other value is
	// JSON-encoded. Save overwrites any existing record at key.
	Save(key string, data any) error

	// Load reads the record at key into out and reports whether key
	// existed. out must be a pointer: *string for a record saved as
	// a string, or a pointer to a JSON-compatible type otherwise. A
	// missing key reports (false, nil) and leaves out untouched.
	Load(key string, out any) (bool, error)

	// List returns every key stored under prefix, sorted
	// lexicographically. An empty prefix lists every key. A prefix
	// with no matching records returns an empty, non-nil slice.
	List(prefix string) ([]string, error)

	// ListChildren returns the keys of the records stored directly
	// under prefix, sorted lexicographically. Records nested deeper
	// below prefix are not returned. A prefix with no records, or one
	// that disappears while it is listed, returns an empty, non-nil
	// slice.
	ListChildren(prefix string) ([]string, error)

	// Delete removes the record at key. Deleting a key that does not
	// exist is a no-op, not an error.
	Delete(key string) error
}

// InMemoryStorage is a Storage backed by a map, for tests and
// ephemeral sessions. Every record round-trips through JSON so its
// read/write contract matches FileStorage's exactly, including which
// out types Load accepts. InMemoryStorage is safe for concurrent use.
type InMemoryStorage struct {
	mu    sync.Mutex
	store map[string]json.RawMessage
}

// NewInMemoryStorage constructs an empty InMemoryStorage.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{store: make(map[string]json.RawMessage)}
}

func (m *InMemoryStorage) Save(key string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = raw
	return nil
}

func (m *InMemoryStorage) Load(key string, out any) (bool, error) {
	m.mu.Lock()
	raw, ok := m.store[key]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, err
	}
	return true, nil
}

func (m *InMemoryStorage) List(prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *InMemoryStorage) ListChildren(prefix string) ([]string, error) {
	dir := strings.TrimSuffix(prefix, "/") + "/"
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		if rest, ok := strings.CutPrefix(k, dir); ok && rest != "" && !strings.Contains(rest, "/") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *InMemoryStorage) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, key)
	return nil
}
