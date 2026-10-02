package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func fileBackedServer(t *testing.T, root string) (*storage.FileStorage, func(string, map[string]any) map[string]any) {
	t.Helper()
	store, err := storage.NewFileStorage(storage.FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	cs, ctx := testServer(t, ServerDeps{Probe: device.NewStaticProbe(), Factory: device.NewMockDriverFactory(), Storage: store})
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
	store, call := fileBackedServer(t, root)

	finished := int64(1_789_000_004_000)
	legacy := storage.PersistedRun{
		ID: "run-1789000000000-1", Name: "legacy run", Source: "exploratory", Status: storage.RunStatusPassed,
		StartedAt: 1_789_000_000_000, FinishedAt: &finished, DurationMs: 4000, ActionCount: 3, Findings: []storage.RunBugReport{},
	}
	stale := storage.PersistedRun{
		ID: "run-1788000000000-1", Name: "stale run", Source: "exploratory", Status: storage.RunStatusRunning,
		StartedAt: 1_788_000_000_000, Findings: []storage.RunBugReport{},
	}
	shadowed := storage.PersistedRun{
		ID: "20261002-101500-mcp-a1b2", Name: "legacy copy", Source: "exploratory", Status: storage.RunStatusPassed,
		StartedAt: 1_700_000_000_000, FinishedAt: &finished, Findings: []storage.RunBugReport{},
	}
	for _, r := range []storage.PersistedRun{legacy, stale, shadowed} {
		if err := store.Save(storage.RunKey(r.ID), r); err != nil {
			t.Fatal(err)
		}
	}
	writeV1Run(t, root, finishedV1Meta("20261002-101500-mcp-a1b2", 1_790_000_000_000))

	listed := call("list_runs", map[string]any{})
	got, _ := json.MarshalIndent(listed, "", "  ")
	got = append(got, '\n')
	got2, _ := json.MarshalIndent(call("get_run", map[string]any{"id": "20261002-101500-mcp-a1b2"}), "", "  ")
	got = append(got, got2...)
	got = append(got, '\n')
	legacyGet, _ := json.MarshalIndent(call("get_run", map[string]any{"id": stale.ID}), "", "  ")
	got = append(got, legacyGet...)
	got = append(got, '\n')

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
	if runs := listed["runs"].([]any); len(runs) != 3 || listed["totalMatching"] != float64(3) {
		t.Fatalf("merged list: got %v, want 3 runs with the v1 copy winning", listed)
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
