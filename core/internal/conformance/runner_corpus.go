package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dev.atomyx/internal/wire"
)

// RunnerCapabilitiesOverride is a partial override of the mock
// driver's default Capabilities, applied field by field: a nil field
// leaves that capability at the mock driver's default, while a
// non-nil field replaces it.
type RunnerCapabilitiesOverride struct {
	CanScreenshot     *bool    `json:"canScreenshot,omitempty"`
	CanEraseText      *bool    `json:"canEraseText,omitempty"`
	CanWaitForIdle    *bool    `json:"canWaitForIdle,omitempty"`
	CanSetLocation    *bool    `json:"canSetLocation,omitempty"`
	CanSetOrientation *bool    `json:"canSetOrientation,omitempty"`
	CanHideKeyboard   *bool    `json:"canHideKeyboard,omitempty"`
	CanMultiPointer   *bool    `json:"canMultiPointer,omitempty"`
	CanPressure       *bool    `json:"canPressure,omitempty"`
	SupportedKeyCodes []string `json:"supportedKeyCodes,omitempty"`
}

// RunnerDriverProgram is the declarative MockDriver program a corpus
// case stages: the ordered hierarchy queue every case must supply,
// plus optional capability and screen-size overrides.
type RunnerDriverProgram struct {
	Hierarchies  []wire.TreeNodeWire         `json:"hierarchies"`
	Capabilities *RunnerCapabilitiesOverride `json:"capabilities,omitempty"`
	Screen       *wire.Size                  `json:"screen,omitempty"`

	// Apps lists the app ids the mock driver considers installed,
	// for a case whose script or replay launches an app. A case that
	// omits it stages no installed apps, matching a device that
	// reports zero.
	Apps []string `json:"apps,omitempty"`
}

// RunnerNetworkRequest is one captured request the fake NetworkCapture
// serves to a `capture` step, matching CapturedRequest's shape.
type RunnerNetworkRequest struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        any               `json:"body,omitempty"`
	TimestampMs int64             `json:"timestampMs,omitempty"`
}

// RunnerNetworkProgram is the declarative fake NetworkCapture program
// a corpus case stages. A case with no RunnerNetworkProgram runs with
// no NetworkCapture adapter configured at all.
type RunnerNetworkProgram struct {
	Requests []RunnerNetworkRequest `json:"requests"`
}

// RunnerCase is one differential script-runner case read from a
// corpus file under corpus/runner/. The corpus file format,
// including the `{{DIR}}` flow-file placeholder convention, is
// documented in corpus/runner/format.md.
type RunnerCase struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Env         map[string]string     `json:"env,omitempty"`
	Yaml        string                `json:"yaml"`
	FlowFiles   map[string]string     `json:"flowFiles,omitempty"`
	Driver      RunnerDriverProgram   `json:"driver"`
	Network     *RunnerNetworkProgram `json:"network,omitempty"`
	// ExpectRunError marks a case where loading the script's own
	// required-flow file fails: the reference oracle process crashes
	// with an uncaught exception instead of producing a normalized
	// ScriptResult, so the Go runner's Run must return a non-nil
	// error rather than a ScriptResult for the same case. The
	// differential test asserts both sides failed to complete
	// instead of diffing a ScriptResult, since neither side has one.
	ExpectRunError bool `json:"expectRunError,omitempty"`
}

// ResolveDir returns a copy of c with every occurrence of the
// `{{DIR}}` placeholder in Yaml and in every FlowFiles entry replaced
// by dir, the absolute path of the case's temporary directory. See
// corpus/runner/format.md for why both runners resolve flow-file
// references against one shared directory this way.
func (c *RunnerCase) ResolveDir(dir string) RunnerCase {
	resolved := *c
	resolved.Yaml = strings.ReplaceAll(c.Yaml, "{{DIR}}", dir)
	if c.FlowFiles != nil {
		resolved.FlowFiles = make(map[string]string, len(c.FlowFiles))
		for name, src := range c.FlowFiles {
			resolved.FlowFiles[name] = strings.ReplaceAll(src, "{{DIR}}", dir)
		}
	}
	return resolved
}

// WriteFlowFiles writes every entry of c.FlowFiles under dir, creating
// any intermediate directories a name's own path segments name.
func (c *RunnerCase) WriteFlowFiles(dir string) error {
	for name, src := range c.FlowFiles {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("creating directory for flow file %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			return fmt.Errorf("writing flow file %s: %w", name, err)
		}
	}
	return nil
}

func loadRunnerCase(path string) (*RunnerCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading runner corpus case %s: %w", path, err)
	}
	var c RunnerCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing runner corpus case %s: %w", path, err)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if c.Name == "" {
		return nil, fmt.Errorf("runner corpus case %s: missing name", path)
	}
	if c.Name != stem {
		return nil, fmt.Errorf("runner corpus case %s: name %q does not match filename stem %q", path, c.Name, stem)
	}
	if c.Description == "" {
		return nil, fmt.Errorf("runner corpus case %s: missing description", path)
	}
	if c.Yaml == "" {
		return nil, fmt.Errorf("runner corpus case %s: missing yaml", path)
	}
	if len(c.Driver.Hierarchies) == 0 {
		return nil, fmt.Errorf("runner corpus case %s: driver.hierarchies must have at least one tree", path)
	}

	return &c, nil
}

// LoadRunnerCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadRunnerCorpus(dir string) ([]*RunnerCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading runner corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*RunnerCase, 0, len(names))
	for _, name := range names {
		c, err := loadRunnerCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
