package mcp

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func keyedScriptRun(id, key string, result runfolder.Result, startedAt, durationMs int64) runfolder.Meta {
	completed := startedAt + durationMs
	return runfolder.Meta{
		SchemaVersion: runfolder.SchemaVersion, RunID: id, Kind: runfolder.KindScript,
		TestKey: key, StartedAt: startedAt, CompletedAt: &completed, Result: result,
	}
}

func statsKeys(decoded map[string]any) []string {
	var keys []string
	for _, entry := range decoded["tests"].([]any) {
		keys = append(keys, entry.(map[string]any)["testKey"].(string))
	}
	return keys
}

func seedStatsRuns(t *testing.T, root string) {
	t.Helper()
	const base = int64(1_790_000_000_000)
	runs := []runfolder.Meta{
		keyedScriptRun("20261001-100000-run-0001", "checkout.yml", runfolder.ResultFailed, base+1000, 3000),
		keyedScriptRun("20261001-100001-run-0002", "checkout.yml", runfolder.ResultPassed, base+2000, 1000),
		keyedScriptRun("20261001-100002-run-0003", "checkout.yml", runfolder.ResultPassed, base+3000, 2000),
		keyedScriptRun("20261001-100003-run-0004", "login.yml", runfolder.ResultPassed, base+4000, 500),
	}
	for _, m := range runs {
		writeV1Run(t, root, m)
	}
}

func TestGetTestStatsReportsPerTestStatisticsFlakiestFirst(t *testing.T) {
	root := t.TempDir()
	_, call := fileBackedServer(t, root)
	seedStatsRuns(t, root)

	got := call("get_test_stats", map[string]any{})
	if got["ok"] != true || got["skipped"] != float64(0) {
		t.Fatalf("get_test_stats: %v", got)
	}
	if keys := statsKeys(got); !reflect.DeepEqual(keys, []string{"checkout.yml", "login.yml"}) {
		t.Fatalf("keys = %v", keys)
	}
	checkout := got["tests"].([]any)[0].(map[string]any)
	want := map[string]any{
		"testKey": "checkout.yml", "runs": float64(3), "passed": float64(2), "failed": float64(1),
		"flaky": float64(0), "flakyRate": float64(0),
		"lastOutcomes":  []any{"passed", "passed", "failed"},
		"durationP50Ms": float64(2000), "durationP95Ms": float64(3000),
	}
	if !reflect.DeepEqual(checkout, want) {
		t.Fatalf("checkout = %v, want %v", checkout, want)
	}
}

func TestGetTestStatsAppliesFilters(t *testing.T) {
	root := t.TempDir()
	_, call := fileBackedServer(t, root)
	seedStatsRuns(t, root)

	tests := []struct {
		name     string
		args     map[string]any
		wantKeys []string
		wantRuns float64
	}{
		{"test key", map[string]any{"test": "login.yml"}, []string{"login.yml"}, 1},
		{"unknown key", map[string]any{"test": "none.yml"}, nil, 0},
		{"last", map[string]any{"test": "checkout.yml", "last": float64(2)}, []string{"checkout.yml"}, 2},
		{"since date after all runs", map[string]any{"since": "2099-01-01"}, nil, 0},
		{"since timestamp before all runs", map[string]any{"test": "login.yml", "since": "2000-01-01T00:00:00Z"}, []string{"login.yml"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := call("get_test_stats", tc.args)
			if got["ok"] != true {
				t.Fatalf("got %v", got)
			}
			if keys := statsKeys(got); !reflect.DeepEqual(keys, tc.wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, tc.wantKeys)
			}
			if len(tc.wantKeys) > 0 {
				if runs := got["tests"].([]any)[0].(map[string]any)["runs"]; runs != tc.wantRuns {
					t.Fatalf("runs = %v, want %v", runs, tc.wantRuns)
				}
			}
		})
	}
}

func TestGetTestStatsOnEmptyStorageReturnsEmptyTests(t *testing.T) {
	_, call := fileBackedServer(t, t.TempDir())
	got := call("get_test_stats", map[string]any{})
	tests, ok := got["tests"].([]any)
	if got["ok"] != true || !ok || len(tests) != 0 {
		t.Fatalf("got %v, want ok with an empty tests array", got)
	}
}

func TestGetTestStatsCountsUnreadableRunsAsSkipped(t *testing.T) {
	root := t.TempDir()
	_, call := fileBackedServer(t, root)
	seedStatsRuns(t, root)
	damaged := runfolder.RunDir(root, "20261001-100009-run-0009")
	writeV1Run(t, root, keyedScriptRun("20261001-100009-run-0009", "x.yml", runfolder.ResultPassed, 1, 1))
	if err := writeFile(damaged+"/"+runfolder.MetaFileName, "{not json"); err != nil {
		t.Fatal(err)
	}
	got := call("get_test_stats", map[string]any{})
	if got["skipped"] != float64(1) {
		t.Fatalf("skipped = %v, want 1: %v", got["skipped"], got)
	}
}

func TestGetTestStatsRejectsBadArguments(t *testing.T) {
	_, call := fileBackedServer(t, t.TempDir())
	bad := []map[string]any{
		{"last": float64(0)},
		{"last": float64(1.5)},
		{"last": "3"},
		{"since": "last tuesday"},
		{"since": float64(5)},
		{"test": float64(5)},
	}
	for i, args := range bad {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := call("get_test_stats", args); got["code"] != "INVALID_ARGUMENT" {
				t.Fatalf("args %v: got %v, want INVALID_ARGUMENT", args, got)
			}
		})
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}
