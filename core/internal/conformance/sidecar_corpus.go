package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"dev.atomyx/internal/device"
)

// SidecarStep is one JSON-RPC call replayed against both sidecars in
// order, with no recorded baseline: the differential test diffs the
// two engines' live responses (and the events each accumulates)
// against each other, the same live-diff shape LoadDriverCorpus and
// LoadNormalizerCorpus use. The corpus file format is documented in
// corpus/sidecar/format.md.
type SidecarStep struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// SidecarConcurrentStop describes the one non-linear case the
// differential test supports: a runScript call left in flight long
// enough for a concurrent stopScript to cancel it mid-run, which is
// the only device-free path that produces a genuine runErrored event
// rather than a graceful failed-step runCompleted (see
// corpus/sidecar/format.md for why). StepDelayMs must be large enough
// that StopAfterMs reliably lands before the script's inter-step
// delay elapses on both a subprocess-spoken TypeScript sidecar and an
// in-process Go one; the corpus case that sets this field documents
// the margin its two literal values were chosen for.
type SidecarConcurrentStop struct {
	Yaml        string `json:"yaml"`
	StepDelayMs int    `json:"stepDelayMs"`
	StopAfterMs int    `json:"stopAfterMs"`
}

// SidecarCase is one differential case read from a corpus file under
// corpus/sidecar/. Exactly one of Steps or ConcurrentStop is set.
type SidecarCase struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	DeviceSnapshots [][]device.Descriptor  `json:"deviceSnapshots,omitempty"`
	Driver          *RunnerDriverProgram   `json:"driver,omitempty"`
	Steps           []SidecarStep          `json:"steps,omitempty"`
	ConcurrentStop  *SidecarConcurrentStop `json:"concurrentStop,omitempty"`
}

func loadSidecarCase(path string) (*SidecarCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar corpus case %s: %w", path, err)
	}
	var c SidecarCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing sidecar corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("sidecar corpus case %s: missing name", path)
	}
	hasSteps := len(c.Steps) > 0
	hasConcurrentStop := c.ConcurrentStop != nil
	if hasSteps == hasConcurrentStop {
		return nil, fmt.Errorf("sidecar corpus case %s: must set exactly one of steps or concurrentStop", path)
	}
	return &c, nil
}

// LoadSidecarCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadSidecarCorpus(dir string) ([]*SidecarCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*SidecarCase, 0, len(names))
	for _, name := range names {
		c, err := loadSidecarCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
