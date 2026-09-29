package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ScriptCase is one differential parse-and-validate case read from a
// corpus file under corpus/script/. The corpus file format is
// documented in corpus/script/format.md.
type ScriptCase struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Yaml            string            `json:"yaml"`
	ExternalEnv     map[string]string `json:"externalEnv,omitempty"`
	ExpectErrorText bool              `json:"expectErrorText,omitempty"`
}

func loadScriptCase(path string) (*ScriptCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading script corpus case %s: %w", path, err)
	}
	var c ScriptCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing script corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("script corpus case %s: missing name", path)
	}
	// c.Yaml may legitimately be "", which is itself a case (an empty
	// script must be rejected the same way by both implementations).
	return &c, nil
}

// LoadScriptCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadScriptCorpus(dir string) ([]*ScriptCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading script corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*ScriptCase, 0, len(names))
	for _, name := range names {
		c, err := loadScriptCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
