package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Step is a single JSON-RPC call within a Case, together with the
// baseline value that call is expected to produce. The corpus file
// format is documented in corpus/format.md.
type Step struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Expect json.RawMessage `json:"expect"`
}

// Case is an ordered sequence of JSON-RPC calls read from a corpus
// file. The corpus file format is documented in corpus/format.md.
type Case struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
}

// LoadCase reads and validates a single corpus case file.
func LoadCase(path string) (*Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading corpus case %s: %w", path, err)
	}
	var c Case
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("corpus case %s: missing name", path)
	}
	if len(c.Steps) == 0 {
		return nil, fmt.Errorf("corpus case %s: has no steps", path)
	}
	for i, step := range c.Steps {
		if step.Method == "" {
			return nil, fmt.Errorf("corpus case %s: step %d has no method", path, i)
		}
	}
	return &c, nil
}

// LoadCorpus reads every *.json case file directly under dir, sorted
// by filename for deterministic execution order.
func LoadCorpus(dir string) ([]*Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*Case, 0, len(names))
	for _, name := range names {
		c, err := LoadCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
