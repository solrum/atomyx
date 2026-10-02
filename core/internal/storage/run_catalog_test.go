package storage

import (
	"errors"
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
	if m := byID["r"]; m.Result != "" || m.Outcome != "" || m.CompletedAt != nil {
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

	listing, err := NewRunCatalog(fs, root, nil).List()
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
	if m, _, _ := NewRunCatalog(fs, root, nil).Get("shared"); m.Outcome != runfolder.OutcomeFailed {
		t.Fatalf("shared id resolved to %+v, want the versioned run", m)
	}
}

func TestRunCatalogServesLegacyAloneOverAnUnrootedStore(t *testing.T) {
	store := NewInMemoryStorage()
	saveLegacy(t, store, PersistedRun{ID: "a", Status: RunStatusPassed})
	c := NewRunCatalog(store, "", nil)
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

type liveSet map[string]bool

func (l liveSet) IsLive(id string) bool { return l[id] }

func TestRunCatalogSettlesUnfinishedRunsByLiveness(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	saveLegacy(t, fs, PersistedRun{ID: "legacy-live", Status: RunStatusRunning, StartedAt: 1})
	saveLegacy(t, fs, PersistedRun{ID: "legacy-dead", Status: RunStatusRunning, StartedAt: 2})
	writeVersionedRun(t, root, "v-live", 3, false)
	writeVersionedRun(t, root, "v-dead", 4, false)
	c := NewRunCatalog(fs, root, liveSet{"legacy-live": true, "v-live": true})

	for id, live := range map[string]bool{"legacy-live": true, "legacy-dead": false, "v-live": true, "v-dead": false} {
		m, ok, err := c.Get(id)
		if !ok || err != nil {
			t.Fatalf("Get(%s): %v %v", id, ok, err)
		}
		if live && (m.Result != "" || m.Outcome != "") {
			t.Errorf("%s is live, got result %q outcome %q", id, m.Result, m.Outcome)
		}
		if !live && (m.Result != runfolder.ResultCancelled || m.Outcome != runfolder.OutcomeInterrupted) {
			t.Errorf("%s is not live, got result %q outcome %q", id, m.Result, m.Outcome)
		}
	}
	listing, err := c.List()
	if err != nil || len(listing.Runs) != 4 {
		t.Fatalf("List: %v %+v", err, listing)
	}
	for _, m := range listing.Runs {
		if runfolder.LegacyStatusOf(m) == "running" != (m.RunID == "legacy-live" || m.RunID == "v-live") {
			t.Errorf("%s listed with result %q", m.RunID, m.Result)
		}
	}
}

func TestRunCatalogGetFallsBackFromADamagedFolderAndNamesTheRun(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	finished := int64(9)
	writeVersionedRun(t, root, "both", 1, true)
	writeVersionedRun(t, root, "only", 1, true)
	for _, id := range []string{"both", "only"} {
		if err := os.WriteFile(filepath.Join(runfolder.RunDir(root, id), runfolder.MetaFileName), []byte(`{`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saveLegacy(t, fs, PersistedRun{ID: "both", Status: RunStatusPassed, StartedAt: 1, FinishedAt: &finished})
	c := NewRunCatalog(fs, root, nil)

	if m, ok, err := c.Get("both"); err != nil || !ok || m.Outcome != runfolder.OutcomePassed {
		t.Fatalf("fallback: %+v %v %v", m, ok, err)
	}
	_, ok, err := c.Get("only")
	var damaged *DamagedRunError
	if ok || !errors.As(err, &damaged) || damaged.ID != "only" {
		t.Fatalf("damaged only: ok %v err %v, want a DamagedRunError naming the run", ok, err)
	}
	listing, err := c.List()
	if err != nil || len(listing.Runs) != 1 || listing.Runs[0].RunID != "both" ||
		len(listing.Skipped) != 1 || listing.Skipped[0].ID != "only" {
		t.Fatalf("List: runs %+v skipped %+v err %v", listing.Runs, listing.Skipped, err)
	}
}

func TestRunCatalogRejectsARecordWhoseIdDiffersFromItsKey(t *testing.T) {
	store := NewInMemoryStorage()
	if err := store.Save(RunKey("a"), PersistedRun{ID: "b", Status: RunStatusPassed}); err != nil {
		t.Fatal(err)
	}
	c := NewRunCatalog(store, "", nil)
	if _, ok, err := c.Get("a"); ok || err == nil {
		t.Fatalf("Get: ok %v err %v, want a damaged error", ok, err)
	}
	listing, err := c.List()
	if err != nil || len(listing.Runs) != 0 || len(listing.Skipped) != 1 {
		t.Fatalf("List: %+v %v", listing, err)
	}
}

type ghostStorage struct {
	Storage
	ghosts []string
}

func (g ghostStorage) ListChildren(prefix string) ([]string, error) {
	keys, err := g.Storage.ListChildren(prefix)
	return append(keys, g.ghosts...), err
}

func TestLegacyRunListIsShallowAndSurvivesVanishingRecords(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	saveLegacy(t, fs, PersistedRun{ID: "a", Status: RunStatusPassed})
	writeVersionedRun(t, root, "v", 1, true)

	runs, skipped, err := NewLegacyRunReader(ghostStorage{fs, []string{RunKey("vanished")}}).List()
	if err != nil || len(skipped) != 0 || len(runs) != 1 || runs[0].RunID != "a" {
		t.Fatalf("got runs %+v skipped %+v err %v", runs, skipped, err)
	}
	if err := os.RemoveAll(filepath.Join(root, "runs")); err != nil {
		t.Fatal(err)
	}
	if runs, skipped, err := NewLegacyRunReader(fs).List(); err != nil || len(runs) != 0 || len(skipped) != 0 {
		t.Fatalf("missing runs directory: %+v %+v %v", runs, skipped, err)
	}
}

func TestRunCatalogSetSummary(t *testing.T) {
	root := t.TempDir()
	fs, _ := NewFileStorage(FileStorageOptions{Root: root})
	saveLegacy(t, fs, PersistedRun{ID: "a", Status: RunStatusPassed, Name: "keep"})
	writeVersionedRun(t, root, "v", 1, true)
	c := NewRunCatalog(fs, root, nil)

	if ok, err := c.SetSummary("a", "new"); !ok || err != nil {
		t.Fatalf("SetSummary: %v %v", ok, err)
	}
	var got PersistedRun
	if _, err := fs.Load(RunKey("a"), &got); err != nil || got.Summary == nil || *got.Summary != "new" || got.Name != "keep" {
		t.Fatalf("record after SetSummary: %+v %v", got, err)
	}
	if ok, err := c.SetSummary("missing", "x"); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
	if _, err := c.SetSummary("v", "x"); !errors.Is(err, ErrRunReadOnly) {
		t.Fatalf("versioned run: got %v, want ErrRunReadOnly", err)
	}
	before, _ := os.ReadFile(filepath.Join(runfolder.RunDir(root, "v"), runfolder.MetaFileName))
	for _, id := range []string{"v1/v/meta", "../x", `a\b`, ""} {
		if _, err := c.SetSummary(id, "x"); !errors.Is(err, ErrInvalidRunID) {
			t.Fatalf("SetSummary(%q): got %v, want ErrInvalidRunID", id, err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(runfolder.RunDir(root, "v"), runfolder.MetaFileName))
	if string(before) != string(after) {
		t.Fatal("a traversal id changed a folder meta")
	}
}
