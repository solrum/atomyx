package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// McpStep is one MCP tools/call replayed against both servers in
// order, with no recorded baseline: the differential test diffs the
// two engines' live responses against each other, the same live-diff
// shape the sidecar and driver corpora use. The corpus file format is
// documented in corpus/mcp/format.md.
type McpStep struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

// McpCase is one differential case read from a corpus file under
// corpus/mcp/. A case that never calls select_device leaves Driver
// unset; one that does must supply Driver so both engines' MockDriver
// resolves the same screen.
type McpCase struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Driver      *McpDriverProgram `json:"driver,omitempty"`
	// Pinned starts the server bound to one device, as
	// atomyx-mcp --platform <platform> --device <id> does.
	Pinned *McpPinnedDevice `json:"pinned,omitempty"`
	Steps  []McpStep        `json:"steps"`
}

// McpPinnedDevice names the device a case's server is bound to.
type McpPinnedDevice struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
}

func loadMcpCase(path string) (*McpCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading mcp corpus case %s: %w", path, err)
	}
	var c McpCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing mcp corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("mcp corpus case %s: missing name", path)
	}
	if len(c.Steps) == 0 {
		return nil, fmt.Errorf("mcp corpus case %s: must have at least one step", path)
	}
	return &c, nil
}

// LoadMcpCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadMcpCorpus(dir string) ([]*McpCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading mcp corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*McpCase, 0, len(names))
	for _, name := range names {
		c, err := loadMcpCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
