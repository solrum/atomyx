package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// goldenUpdateMode, when true, makes checkGolden overwrite each
// golden fixture with the value it is handed instead of comparing
// against the one already on disk. golden_test.go's -update flag
// sets this once, before any test runs.
var goldenUpdateMode bool

// goldenPath returns the on-disk path for a golden fixture named by
// rel (a path relative to the goldens/ directory root), for example
// "config/happy-path.json" or "mcp/tool-schema.json".
func goldenPath(rel string) string {
	return filepath.Join("goldens", filepath.FromSlash(rel))
}

// checkGolden compares got against the frozen fixture at
// goldens/<relPath>, using cmp the same way every suite already
// compares a live pair (DiffJSON or DiffRunnerJSON both match cmp's
// signature). When goldenUpdateMode is set, it instead (re)writes got
// to that path and returns without comparing, so a subsequent
// non-update run reads back exactly what this run produced.
//
// got is pretty-printed with two-space indentation before either the
// write or the read-compare path, so a golden file is always stored
// and reviewed in the same stable shape regardless of how the
// suite's own marshaling happened to order or space it.
func checkGolden(t *testing.T, relPath string, got []byte, cmp func(want, got []byte) (string, bool, error)) {
	t.Helper()

	pretty, err := prettyGoldenJSON(got)
	if err != nil {
		t.Fatalf("pretty-printing golden %s: %v", relPath, err)
	}
	path := goldenPath(relPath)

	if goldenUpdateMode {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating golden directory for %s: %v", relPath, err)
		}
		if err := os.WriteFile(path, pretty, 0o644); err != nil {
			t.Fatalf("writing golden %s: %v", relPath, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run `go test ./internal/conformance/... -update` to regenerate conformance goldens): %v", relPath, err)
	}

	report, equal, err := cmp(want, pretty)
	if err != nil {
		t.Fatalf("comparing golden %s: %v", relPath, err)
	}
	if !equal {
		t.Errorf("%s: Go output does not match the frozen golden (- golden, + Go):\n%s", relPath, report)
	}
}

// prettyGoldenJSON decodes raw as JSON and re-encodes it with
// two-space indentation and a trailing newline, so every golden file
// in the tree has one stable on-disk shape regardless of which
// suite produced it.
func prettyGoldenJSON(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decoding JSON to pretty-print: %w", err)
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling pretty JSON: %w", err)
	}
	return append(pretty, '\n'), nil
}
