package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// NormalizerCase is one differential tree-normalizer case read from a
// corpus file. The corpus file format is documented in
// corpus/format.md.
type NormalizerCase struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	RawTree     json.RawMessage `json:"rawTree"`
}

// loadNormalizerCase reads and validates a single normalizer corpus
// case file.
func loadNormalizerCase(path string) (*NormalizerCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading normalizer corpus case %s: %w", path, err)
	}
	var c NormalizerCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing normalizer corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("normalizer corpus case %s: missing name", path)
	}
	if len(c.RawTree) == 0 {
		return nil, fmt.Errorf("normalizer corpus case %s: missing rawTree", path)
	}
	return &c, nil
}

// LoadNormalizerCorpus reads every *.json case file directly under
// dir, sorted by filename for deterministic execution order.
func LoadNormalizerCorpus(dir string) ([]*NormalizerCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading normalizer corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*NormalizerCase, 0, len(names))
	for _, name := range names {
		c, err := loadNormalizerCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
