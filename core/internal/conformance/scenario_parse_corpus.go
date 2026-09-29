package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ScenarioCase is one differential parse-and-validate case read from a
// corpus file under corpus/scenario_parse/, mirroring ScriptCase.
type ScenarioCase struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Yaml            string `json:"yaml"`
	ExpectErrorText bool   `json:"expectErrorText,omitempty"`
}

func loadScenarioCase(path string) (*ScenarioCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenario corpus case %s: %w", path, err)
	}
	var c ScenarioCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing scenario corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("scenario corpus case %s: missing name", path)
	}
	return &c, nil
}

// LoadScenarioCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadScenarioCorpus(dir string) ([]*ScenarioCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading scenario corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*ScenarioCase, 0, len(names))
	for _, name := range names {
		c, err := loadScenarioCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
