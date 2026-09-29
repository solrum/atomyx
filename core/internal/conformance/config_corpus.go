package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ConfigCase is one differential parse-validate-and-select-environment
// case read from a corpus file under corpus/config/. NoConfig
// simulates ConfigApi.load discovering no config file at all; when it
// is false, Yaml is the config document under test. EnvName mirrors
// an omitted (nil) versus an explicitly given `--env` flag value.
type ConfigCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	NoConfig    bool              `json:"noConfig,omitempty"`
	Yaml        string            `json:"yaml,omitempty"`
	EnvVars     map[string]string `json:"envVars,omitempty"`
	EnvName     *string           `json:"envName,omitempty"`
}

func loadConfigCase(path string) (*ConfigCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config corpus case %s: %w", path, err)
	}
	var c ConfigCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing config corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("config corpus case %s: missing name", path)
	}
	if !c.NoConfig && c.Yaml == "" {
		return nil, fmt.Errorf("config corpus case %s: neither noConfig nor yaml is set", path)
	}
	return &c, nil
}

// LoadConfigCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadConfigCorpus(dir string) ([]*ConfigCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading config corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*ConfigCase, 0, len(names))
	for _, name := range names {
		c, err := loadConfigCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
