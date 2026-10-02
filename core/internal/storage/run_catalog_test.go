package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func saveLegacy(t *testing.T, s Storage, r PersistedRun) {
	t.Helper()
	if err := s.Save(RunKey(r.ID), r); err != nil {
		t.Fatal(err)
	}
}

func writeVersionedRun(t *testing.T, root, id string, startedAt int64, completed bool) {
	t.Helper()
	dir := runfolder.RunDir(root, id)
	if err := os.MkdirAll(runfolder.ArtifactsDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":1,"runId":"` + id + `","kind":"script","startedAt":` + strconv.FormatInt(startedAt, 10)
	if completed {
		body += `,"completedAt":` + strconv.FormatInt(startedAt+10, 10) + `,"outcome":"failed"`
	}
	body += `}`
	if err := os.WriteFile(filepath.Join(dir, runfolder.MetaFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runfolder.ArtifactsDir(dir), "tree.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRunReaderMapsStatusesAndIgnoresNestedKeys(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	finished := int64(2_000)
	summary := "s"
	saveLegacy(t, fs, PersistedRun{ID: "e", Status: RunStatusError, StartedAt: 1_000, FinishedAt: &finished, DurationMs: 1_000, ActionCount: 4, Summary: &summary,
		Findings: []RunBugReport{{ID: "e-bug-1", Title: "t", Timestamp: 1_500}}})
	saveLegacy(t, fs, PersistedRun{ID: "r", Status: RunStatusRunning, StartedAt: 3_000})
	writeVersionedRun(t, root, "v", 5_000, true)

	runs, skipped, err := NewLegacyRunReader(fs).List()
	if err != nil || len(skipped) != 0 {
		t.Fatalf("List: %v %v", skipped, err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d legacy runs, want 2 (the versioned folder is not a legacy record): %+v", len(runs), runs)
	}
	byID := map[string]runfolder.Meta{runs[0].RunID: runs[0], runs[1].RunID: runs[1]}
	if m := byID["e"]; m.Result != runfolder.ResultFailed || m.Outcome != runfolder.OutcomeError || m.CompletedAt == nil || *m.CompletedAt != 2_000 ||
		m.MCP.ActionCount != 4 || *m.MCP.Summary != "s" || len(m.MCP.Findings) != 1 || m.MCP.DurationMs != 1_000 {
		t.Fatalf("error record mapped to %+v", m)
	}
	if m := byID["r"]; m.Result != runfolder.ResultCancelled || m.Outcome != runfolder.OutcomeInterrupted || m.CompletedAt != nil {
		t.Fatalf("running record mapped to %+v", m)
	}
}

func TestLegacyRunReaderGetRejectsNestedIDs(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	writeVersionedRun(t, root, "v", 1, true)
	if _, ok, err := NewLegacyRunReader(fs).Get("v1/v/meta"); ok || err != nil {
		t.Fatalf("got ok %v err %v, want not found", ok, err)
	}
}

func TestLegacyRunReaderReportsAnUnknownStatusAndKeepsTheRest(t *testing.T) {
	store := NewInMemoryStorage()
	saveLegacy(t, store, PersistedRun{ID: "odd", Status: "paused"})
	saveLegacy(t, store, PersistedRun{ID: "ok", Status: RunStatusPassed})

	runs, skipped, err := NewLegacyRunReader(store).List()
	if err != nil || len(skipped) != 1 || len(runs) != 1 || runs[0].RunID != "ok" {
		t.Fatalf("got runs %+v skipped %v err %v", runs, skipped, err)
	}
}

func TestRunCatalogPrefersVersionedAndSortsNewestFirst(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	finished := int64(10)
	saveLegacy(t, fs, PersistedRun{ID: "shared", Status: RunStatusPassed, StartedAt: 1, FinishedAt: &finished})
	saveLegacy(t, fs, PersistedRun{ID: "old", Status: RunStatusPassed, StartedAt: 2, FinishedAt: &finished})
	writeVersionedRun(t, root, "shared", 9, true)
	writeVersionedRun(t, root, "open", 5, false)

	listing, err := NewRunCatalog(fs).List()
	if err != nil || len(listing.Skipped) != 0 {
		t.Fatalf("List: %v %v", listing.Skipped, err)
	}
	var ids []string
	for _, m := range listing.Runs {
		ids = append(ids, m.RunID)
	}
	if len(ids) != 3 || ids[0] != "shared" || ids[1] != "open" || ids[2] != "old" {
		t.Fatalf("order: got %v", ids)
	}
	if m, _, _ := NewRunCatalog(fs).Get("shared"); m.Outcome != runfolder.OutcomeFailed {
		t.Fatalf("shared id resolved to %+v, want the versioned run", m)
	}
}

func TestRunCatalogServesLegacyAloneOverAnUnrootedStore(t *testing.T) {
	store := NewInMemoryStorage()
	saveLegacy(t, store, PersistedRun{ID: "a", Status: RunStatusPassed})
	c := NewRunCatalog(store)
	if _, ok, err := c.Get("a"); !ok || err != nil {
		t.Fatalf("Get: %v %v", ok, err)
	}
	if ok, err := c.Delete("a"); !ok || err != nil {
		t.Fatalf("Delete: %v %v", ok, err)
	}
	if ok, err := c.Delete("a"); ok || err != nil {
		t.Fatalf("second Delete: %v %v, want absent without error", ok, err)
	}
}
