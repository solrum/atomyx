package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func newMigrationRoot(t *testing.T) (string, *FileStorage) {
	t.Helper()
	root := t.TempDir()
	store, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return root, store
}

func strPtr(s string) *string { return &s }

func int64Ptr(n int64) *int64 { return &n }

func legacyFixtures() []PersistedRun {
	return []PersistedRun{
		{ID: "run-passed", Name: "login smoke", Source: "agent", Status: RunStatusPassed, StartedAt: 1_000,
			FinishedAt: int64Ptr(1_900), DurationMs: 900, ActionCount: 7, Summary: strPtr("all green"),
			Findings: []RunBugReport{
				{ID: "run-passed-bug-1", Title: "t1", Description: "d1", ScreenshotPath: "/a.png", Timestamp: 1_100},
				{ID: "run-passed-bug-2", Title: "t2", Description: "d2", Timestamp: 1_200},
			}},
		{ID: "run-failed", Name: "checkout", Source: "script", Status: RunStatusFailed, StartedAt: 2_000,
			FinishedAt: int64Ptr(2_500), DurationMs: 500, ActionCount: 3, Summary: strPtr(""),
			Findings: []RunBugReport{}},
		{ID: "run-error", Name: "boot", Source: "agent", Status: RunStatusError, StartedAt: 3_000,
			FinishedAt: int64Ptr(3_050), DurationMs: 50, ActionCount: 0},
		{ID: "run-cancelled", Name: "idle", Source: "unknown", Status: RunStatusCancelled, StartedAt: 4_000,
			DurationMs: 250, ActionCount: 1},
		{ID: "run-running", Name: "live", Source: "agent", Status: RunStatusRunning, StartedAt: 5_000,
			ActionCount: 2, Summary: strPtr("in flight")},
	}
}

func saveAll(t *testing.T, store Storage, records []PersistedRun) {
	t.Helper()
	for _, r := range records {
		saveLegacy(t, store, r)
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		if filepath.Base(path) == runfolder.MigrateLockFileName {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertFieldsPreserved(t *testing.T, root string, record PersistedRun) {
	t.Helper()
	meta, ok, err := runfolder.NewReader(root).Get(record.ID)
	if err != nil || !ok {
		t.Fatalf("%s: no v1 folder: ok=%v err=%v", record.ID, ok, err)
	}
	if meta.SchemaVersion != runfolder.SchemaVersion || meta.Kind != runfolder.KindMCP || meta.RunID != record.ID {
		t.Fatalf("%s: identity = %+v", record.ID, meta)
	}
	if meta.StartedAt != record.StartedAt {
		t.Fatalf("%s: startedAt = %d, want %d", record.ID, meta.StartedAt, record.StartedAt)
	}
	if got := runfolder.LegacyStatusOf(meta); got != string(record.Status) {
		t.Fatalf("%s: status round trip = %q, want %q", record.ID, got, record.Status)
	}
	if record.Status == RunStatusRunning {
		if meta.CompletedAt != nil || meta.Result != "" || meta.Outcome != "" {
			t.Fatalf("%s: a running record must stay unfinished: %+v", record.ID, meta)
		}
	} else {
		if meta.CompletedAt == nil || meta.Result == "" || meta.Outcome == "" {
			t.Fatalf("%s: a finished record needs completedAt, result, and outcome: %+v", record.ID, meta)
		}
		want := record.StartedAt + record.DurationMs
		if record.FinishedAt != nil {
			want = *record.FinishedAt
		}
		if *meta.CompletedAt != want {
			t.Fatalf("%s: completedAt = %d, want %d", record.ID, *meta.CompletedAt, want)
		}
	}
	info := meta.MCP
	if info == nil {
		t.Fatalf("%s: mcp block missing", record.ID)
	}
	if info.Name != record.Name || info.Source != record.Source || info.ActionCount != record.ActionCount || info.DurationMs != record.DurationMs {
		t.Fatalf("%s: mcp scalars = %+v", record.ID, info)
	}
	switch {
	case record.Summary == nil && info.Summary != nil:
		t.Fatalf("%s: an absent summary became %q", record.ID, *info.Summary)
	case record.Summary != nil && (info.Summary == nil || *info.Summary != *record.Summary):
		t.Fatalf("%s: summary %q was lost", record.ID, *record.Summary)
	}
	if len(info.Findings) != len(record.Findings) {
		t.Fatalf("%s: %d findings, want %d", record.ID, len(info.Findings), len(record.Findings))
	}
	for i, f := range record.Findings {
		got := info.Findings[i]
		if got.ID != f.ID || got.Title != f.Title || got.Description != f.Description || got.ScreenshotPath != f.ScreenshotPath || got.Timestamp != f.Timestamp {
			t.Fatalf("%s: finding %d = %+v, want %+v", record.ID, i, got, f)
		}
	}
	if _, err := os.Stat(runfolder.LockPath(runfolder.RunDir(root, record.ID))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s: a migrated folder must carry no run.lock: %v", record.ID, err)
	}
}

func TestMigrateRunsMovesEveryRecordIntoAFolderAndLosesNoField(t *testing.T) {
	root, store := newMigrationRoot(t)
	records := legacyFixtures()
	saveAll(t, store, records)
	writeVersionedRun(t, root, "20260101-000000-script-aaaa", 9_000, true)

	report, err := MigrateRuns(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Migrated) != len(records) || len(report.AlreadyMigrated) != 0 || len(report.Conflicts) != 0 || len(report.Damaged) != 0 || !report.Clean() {
		t.Fatalf("report = %+v", report)
	}
	for _, r := range records {
		assertFieldsPreserved(t, root, r)
		if ok, err := store.Load(RunKey(r.ID), &PersistedRun{}); ok || err != nil {
			t.Fatalf("%s: original still there: %v %v", r.ID, ok, err)
		}
	}
	if _, ok, _ := runfolder.NewReader(root).Get("20260101-000000-script-aaaa"); !ok {
		t.Fatal("a folder that was already a v1 run was disturbed")
	}

	before := snapshotTree(t, root)
	again, err := MigrateRuns(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, MigrationReport{}) {
		t.Fatalf("second report = %+v, want empty", again)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("a second migration changed the tree")
	}
}

func TestMigrateRunsLeavesDamagedRecordsAndConflictsInPlace(t *testing.T) {
	root, store := newMigrationRoot(t)
	saveLegacy(t, store, PersistedRun{ID: "good", Name: "g", Source: "agent", Status: RunStatusPassed, StartedAt: 1})
	saveLegacy(t, store, PersistedRun{ID: "odd-status", Status: RunStatus("exploded"), StartedAt: 2})
	saveLegacy(t, store, PersistedRun{ID: "other-id", Status: RunStatusPassed, StartedAt: 3})
	if err := os.WriteFile(filepath.Join(root, "runs", "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "runs", "other-id.json"), filepath.Join(root, "runs", "renamed.json")); err != nil {
		t.Fatal(err)
	}
	saveLegacy(t, store, PersistedRun{ID: "clash", Name: "from record", Status: RunStatusPassed, StartedAt: 4})
	writeVersionedRun(t, root, "clash", 99, true)

	report, err := MigrateRuns(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Migrated, []string{"good"}) {
		t.Fatalf("migrated = %v", report.Migrated)
	}
	var damaged []string
	for _, d := range report.Damaged {
		if d.Reason == "" {
			t.Fatalf("damaged entry %q has no reason", d.ID)
		}
		damaged = append(damaged, d.ID)
	}
	sort.Strings(damaged)
	if !reflect.DeepEqual(damaged, []string{"broken", "odd-status", "renamed"}) {
		t.Fatalf("damaged = %v", damaged)
	}
	if len(report.Conflicts) != 1 || report.Conflicts[0].ID != "clash" || report.Clean() {
		t.Fatalf("conflicts = %+v", report.Conflicts)
	}
	for _, id := range []string{"odd-status", "renamed", "broken", "clash"} {
		if _, err := os.Stat(filepath.Join(root, "runs", id+".json")); err != nil {
			t.Fatalf("%s original was not left in place: %v", id, err)
		}
	}
	if got, _, _ := runfolder.NewReader(root).Get("clash"); got.StartedAt != 99 {
		t.Fatalf("the conflicting folder was changed: %+v", got)
	}
}

type failingDeleteStore struct {
	Storage
	failKey string
}

func (s *failingDeleteStore) Delete(key string) error {
	if key == s.failKey {
		return errors.New("injected failure before the original is removed")
	}
	return s.Storage.Delete(key)
}

func TestMigrateRunsResumesAfterACrashBetweenTheFolderAndTheDelete(t *testing.T) {
	root, store := newMigrationRoot(t)
	records := legacyFixtures()
	saveAll(t, store, records)
	scratch := filepath.Join(runfolder.VersionedRunsDir(root), ".import-leftover")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}

	crashing := &failingDeleteStore{Storage: store, failKey: RunKey("run-failed")}
	if _, err := MigrateRuns(crashing, root); err == nil {
		t.Fatal("the injected failure was swallowed")
	}
	if ok, _ := store.Load(RunKey("run-failed"), &PersistedRun{}); !ok {
		t.Fatal("the original was removed despite the failure")
	}
	if _, ok, _ := runfolder.NewReader(root).Get("run-failed"); !ok {
		t.Fatal("the folder was not written before the failure")
	}

	report, err := MigrateRuns(store, root)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(report.AlreadyMigrated, "run-failed") || !report.Clean() {
		t.Fatalf("resume report = %+v", report)
	}
	for _, r := range records {
		assertFieldsPreserved(t, root, r)
		if ok, _ := store.Load(RunKey(r.ID), &PersistedRun{}); ok {
			t.Fatalf("%s: original survived the resumed migration", r.ID)
		}
	}
	entries, err := os.ReadDir(runfolder.VersionedRunsDir(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(records) {
		t.Fatalf("v1 holds %d entries, want exactly %d with no duplicates or scratch", len(entries), len(records))
	}
	if report.ScratchRemoved != 0 {
		t.Fatalf("the failed pass already removed the scratch folder, got %d on resume", report.ScratchRemoved)
	}
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

type loadHookStore struct {
	Storage
	onLoad func()
}

func (s *loadHookStore) Load(key string, out any) (bool, error) {
	if s.onLoad != nil {
		hook := s.onLoad
		s.onLoad = nil
		hook()
	}
	return s.Storage.Load(key, out)
}

func TestEditsDuringAMigrationAreRefusedSoNothingIsLostOrResurrected(t *testing.T) {
	root, store := newMigrationRoot(t)
	saveAll(t, store, legacyFixtures())
	catalog := NewRunCatalog(store, root, nil)

	var setErr, deleteErr error
	hooked := &loadHookStore{Storage: store}
	hooked.onLoad = func() {
		_, setErr = catalog.SetSummary("run-passed", "edited mid-migration")
		_, deleteErr = catalog.Delete("run-error")
	}
	if _, err := MigrateRuns(hooked, root); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(setErr, ErrMigrationInProgress) || !errors.Is(deleteErr, ErrMigrationInProgress) {
		t.Fatalf("edit errors = %v, %v, want ErrMigrationInProgress", setErr, deleteErr)
	}
	meta, ok, err := catalog.Get("run-passed")
	if err != nil || !ok || meta.MCP.Summary == nil || *meta.MCP.Summary != "all green" {
		t.Fatalf("the refused edit leaked: %+v %v %v", meta, ok, err)
	}
	if _, ok, _ := catalog.Get("run-error"); !ok {
		t.Fatal("the refused delete removed the run")
	}

	if ok, err := catalog.SetSummary("run-passed", "edited after"); err != nil || !ok {
		t.Fatalf("SetSummary after migration = %v %v", ok, err)
	}
	if ok, err := catalog.Delete("run-error"); err != nil || !ok {
		t.Fatalf("Delete after migration = %v %v", ok, err)
	}
	if _, ok, _ := catalog.Get("run-error"); ok {
		t.Fatal("a deleted run came back")
	}
	again, err := MigrateRuns(store, root)
	if err != nil || len(again.Migrated) != 0 {
		t.Fatalf("a later migration found work: %+v %v", again, err)
	}
	if _, ok, _ := catalog.Get("run-error"); ok {
		t.Fatal("a later migration resurrected a deleted run")
	}
	meta, _, _ = catalog.Get("run-passed")
	if meta.MCP.Summary == nil || *meta.MCP.Summary != "edited after" {
		t.Fatalf("summary = %+v", meta.MCP.Summary)
	}
}

func TestEditsAreRefusedWhileTheMigrationLockIsHeld(t *testing.T) {
	root, _ := newMigrationRoot(t)
	held, err := runfolder.LockMigration(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()
	store, _ := NewFileStorage(FileStorageOptions{Root: root})
	catalog := NewRunCatalog(store, root, nil)
	saveLegacy(t, store, PersistedRun{ID: "r", Status: RunStatusPassed, StartedAt: 1, Summary: strPtr("s")})
	if _, err := catalog.Delete("r"); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatalf("Delete under a held lock = %v", err)
	}
	if _, err := catalog.SetSummary("r", "x"); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatalf("SetSummary under a held lock = %v", err)
	}
	if ok, _ := store.Load(RunKey("r"), &PersistedRun{}); !ok {
		t.Fatal("a refused delete removed the record")
	}
}
