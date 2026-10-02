package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

const updateGoldenEnv = "ATOMYX_UPDATE_GOLDEN"

func writeV1Run(t *testing.T, root string, meta runfolder.Meta) {
	t.Helper()
	dir := runfolder.RunDir(root, meta.RunID)
	artifacts := runfolder.ArtifactsDir(dir)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runfolder.MetaFileName), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "step-001-tree.json"), []byte(`{"nodes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func finishedV1Meta(id string, startedAt int64) runfolder.Meta {
	completed := startedAt + 5000
	summary := "v1 summary"
	return runfolder.Meta{
		SchemaVersion: runfolder.SchemaVersion, RunID: id, Kind: runfolder.KindMCP,
		StartedAt: startedAt, CompletedAt: &completed,
		Result: runfolder.ResultCancelled, Outcome: runfolder.OutcomeDeviceLost,
		MCP: &runfolder.MCPInfo{
			Name: "v1 run", Source: "exploratory", ActionCount: 2, DurationMs: 5000, Summary: &summary,
			Findings: []runfolder.Finding{{ID: id + "-bug-1", Title: "t", Description: "d", Timestamp: startedAt + 1}},
		},
	}
}

type liveRuns map[string]bool

func (l liveRuns) IsLive(id string) bool { return l[id] }

func fileBackedServer(t *testing.T, root string) (*storage.FileStorage, func(string, map[string]any) map[string]any) {
	t.Helper()
	return fileBackedServerWithLiveness(t, root, nil)
}

func fileBackedServerWithLiveness(t *testing.T, root string, live runfolder.Liveness) (*storage.FileStorage, func(string, map[string]any) map[string]any) {
	t.Helper()
	store, err := storage.NewFileStorage(storage.FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	cs, ctx := testServer(t, ServerDeps{
		Probe: device.NewStaticProbe(), Factory: device.NewMockDriverFactory(), Storage: store,
		Runs: storage.NewRunCatalog(store, root, live),
	})
	call := func(name string, args map[string]any) map[string]any {
		_, decoded := callTool(t, cs, ctx, name, args)
		return decoded
	}
	return store, call
}

func TestListRunsReturnsOneRunForAVersionedFolder(t *testing.T) {
	root := t.TempDir()
	_, call := fileBackedServer(t, root)
	writeV1Run(t, root, finishedV1Meta("20261002-101500-mcp-a1b2", 1_790_000_000_000))

	listed := call("list_runs", map[string]any{})
	runs, _ := listed["runs"].([]any)
	if listed["count"] != float64(1) || len(runs) != 1 {
		t.Fatalf("list_runs: got count %v with %d runs, want exactly one run for the folder: %v", listed["count"], len(runs), listed)
	}
	if id := runs[0].(map[string]any)["id"]; id != "20261002-101500-mcp-a1b2" {
		t.Fatalf("list_runs id: got %v", id)
	}
}

func TestRunReadsMergeVersionedAndLegacyRuns(t *testing.T) {
	root := t.TempDir()
	const liveID = "run-1787000000000-1"
	store, call := fileBackedServerWithLiveness(t, root, liveRuns{liveID: true})

	finished := int64(1_789_000_004_000)
	legacy := func(id, name string, status storage.RunStatus, startedAt int64, done bool) storage.PersistedRun {
		r := storage.PersistedRun{ID: id, Name: name, Source: "exploratory", Status: status, StartedAt: startedAt, Findings: []storage.RunBugReport{}}
		if done {
			r.FinishedAt = &finished
			r.DurationMs = 4000
			r.ActionCount = 3
		}
		return r
	}
	records := []storage.PersistedRun{
		legacy("run-1789000000000-1", "legacy passed", storage.RunStatusPassed, 1_789_000_000_000, true),
		legacy("run-1788500000000-1", "legacy failed", storage.RunStatusFailed, 1_788_500_000_000, true),
		legacy("run-1788400000000-1", "legacy error", storage.RunStatusError, 1_788_400_000_000, true),
		legacy("run-1788300000000-1", "legacy cancelled", storage.RunStatusCancelled, 1_788_300_000_000, true),
		legacy("run-1788000000000-1", "stale run", storage.RunStatusRunning, 1_788_000_000_000, false),
		legacy(liveID, "live run", storage.RunStatusRunning, 1_787_000_000_000, false),
		legacy("20261002-101500-mcp-a1b2", "legacy copy", storage.RunStatusPassed, 1_700_000_000_000, true),
	}
	for _, r := range records {
		if err := store.Save(storage.RunKey(r.ID), r); err != nil {
			t.Fatal(err)
		}
	}
	writeV1Run(t, root, finishedV1Meta("20261002-101500-mcp-a1b2", 1_790_000_000_000))

	listed := call("list_runs", map[string]any{"limit": 50})
	got, _ := json.MarshalIndent(listed, "", "  ")
	got = append(got, '\n')
	got2, _ := json.MarshalIndent(call("get_run", map[string]any{"id": "20261002-101500-mcp-a1b2"}), "", "  ")
	got = append(got, got2...)
	got = append(got, '\n')
	for _, id := range []string{"run-1788000000000-1", liveID} {
		body, _ := json.MarshalIndent(call("get_run", map[string]any{"id": id}), "", "  ")
		got = append(got, body...)
		got = append(got, '\n')
	}

	golden := filepath.Join("testdata", "run-payload.golden.json")
	if os.Getenv(updateGoldenEnv) != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("run payloads differ from %s:\n%s", golden, got)
	}
	if runs := listed["runs"].([]any); len(runs) != 7 || listed["totalMatching"] != float64(7) || listed["skipped"] != float64(0) {
		t.Fatalf("merged list: got %v, want 7 runs with the v1 copy winning", listed)
	}
}

func TestDeleteRunRemovesVersionedAndLegacyForms(t *testing.T) {
	root := t.TempDir()
	store, call := fileBackedServer(t, root)
	const id = "20261002-101500-mcp-a1b2"
	writeV1Run(t, root, finishedV1Meta(id, 1_790_000_000_000))
	finished := int64(1_790_000_001_000)
	if err := store.Save(storage.RunKey(id), storage.PersistedRun{ID: id, Status: storage.RunStatusPassed, StartedAt: 1, FinishedAt: &finished, Findings: []storage.RunBugReport{}}); err != nil {
		t.Fatal(err)
	}

	if got := call("delete_run", map[string]any{"id": id}); got["ok"] != true {
		t.Fatalf("delete_run: got %v", got)
	}
	if _, err := os.Stat(runfolder.RunDir(root, id)); !os.IsNotExist(err) {
		t.Fatalf("run folder survived delete_run: %v", err)
	}
	if got := call("get_run", map[string]any{"id": id}); got["ok"] != false {
		t.Fatalf("get_run after delete: got %v, want ok:false", got)
	}
	if got := call("delete_run", map[string]any{"id": id}); got["ok"] != false {
		t.Fatalf("second delete_run: got %v, want ok:false", got)
	}
}

func TestListRunsCountsSkippedRunsApartFromMatches(t *testing.T) {
	root := t.TempDir()
	store, call := fileBackedServer(t, root)
	writeV1Run(t, root, finishedV1Meta("good", 1_790_000_000_000))
	damagedDir := runfolder.RunDir(root, "damaged")
	if err := os.MkdirAll(damagedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(damagedDir, runfolder.MetaFileName), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(storage.RunKey("odd"), storage.PersistedRun{ID: "odd", Status: "paused"}); err != nil {
		t.Fatal(err)
	}

	listed := call("list_runs", map[string]any{})
	if listed["count"] != float64(1) || listed["totalMatching"] != float64(1) || listed["skipped"] != float64(2) {
		t.Fatalf("list_runs: got %v, want one run and two skipped", listed)
	}
}

func TestGetRunOnADamagedFolderFallsBackOrNamesTheRun(t *testing.T) {
	root := t.TempDir()
	store, call := fileBackedServer(t, root)
	for _, id := range []string{"both", "only"} {
		dir := runfolder.RunDir(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, runfolder.MetaFileName), []byte(`{`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	finished := int64(9)
	if err := store.Save(storage.RunKey("both"), storage.PersistedRun{ID: "both", Name: "flat", Status: storage.RunStatusPassed, StartedAt: 1, FinishedAt: &finished, Findings: []storage.RunBugReport{}}); err != nil {
		t.Fatal(err)
	}

	if got := call("get_run", map[string]any{"id": "both"}); got["ok"] != true {
		t.Fatalf("fallback get_run: got %v", got)
	}
	got := call("get_run", map[string]any{"id": "only"})
	if got["code"] != "RUN_UNREADABLE" || !strings.Contains(got["message"].(string), "only") {
		t.Fatalf("damaged get_run: got %v, want RUN_UNREADABLE naming the run", got)
	}
}

func TestUpdateRunSummaryRefusesVersionedRunsAndTraversal(t *testing.T) {
	root := t.TempDir()
	store, call := fileBackedServer(t, root)
	const id = "20261002-101500-mcp-a1b2"
	writeV1Run(t, root, finishedV1Meta(id, 1_790_000_000_000))
	metaPath := filepath.Join(runfolder.RunDir(root, id), runfolder.MetaFileName)
	before, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}

	if got := call("update_run_summary", map[string]any{"id": id, "summary": "x"}); got["code"] != "RUN_READ_ONLY" || !strings.Contains(got["message"].(string), id) {
		t.Fatalf("versioned run: got %v", got)
	}
	if got := call("update_run_summary", map[string]any{"id": "v1/" + id + "/meta", "summary": "x"}); got["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("traversal id: got %v, want INVALID_ARGUMENT", got)
	}
	after, _ := os.ReadFile(metaPath)
	if !bytes.Equal(before, after) {
		t.Fatal("update_run_summary changed a versioned meta")
	}

	if err := store.Save(storage.RunKey("flat"), storage.PersistedRun{ID: "flat", Status: storage.RunStatusPassed, Findings: []storage.RunBugReport{}}); err != nil {
		t.Fatal(err)
	}
	if got := call("update_run_summary", map[string]any{"id": "flat", "summary": "kept"}); got["ok"] != true {
		t.Fatalf("flat run: got %v", got)
	}
}
