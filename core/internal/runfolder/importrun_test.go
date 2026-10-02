package runfolder

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func importMeta(id string, completed bool) Meta {
	m := Meta{SchemaVersion: SchemaVersion, RunID: id, Kind: KindMCP, StartedAt: 1_000}
	if completed {
		done := int64(2_000)
		m.CompletedAt, m.Result, m.Outcome = &done, ResultFailed, OutcomeError
	}
	return m
}

func TestImportRunWritesAFolderWithoutALockAndReadsBack(t *testing.T) {
	root := t.TempDir()
	want := importMeta("legacy-1", true)

	outcome, err := ImportRun(root, want)
	if err != nil || outcome != ImportWritten {
		t.Fatalf("ImportRun = %v %v", outcome, err)
	}
	got, ok, err := NewReader(root).Get("legacy-1")
	if err != nil || !ok || got.RunID != "legacy-1" || got.Outcome != OutcomeError {
		t.Fatalf("read back = %+v %v %v", got, ok, err)
	}
	if _, err := os.Stat(LockPath(RunDir(root, "legacy-1"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an imported run must carry no run.lock: %v", err)
	}
	if names := dirNames(t, VersionedRunsDir(root)); len(names) != 1 {
		t.Fatalf("scratch folders left behind: %v", names)
	}
}

func TestImportRunIsIdempotentForAnIdenticalMetaAndRefusesADifferentOne(t *testing.T) {
	root := t.TempDir()
	if _, err := ImportRun(root, importMeta("r", true)); err != nil {
		t.Fatal(err)
	}
	if outcome, err := ImportRun(root, importMeta("r", true)); err != nil || outcome != ImportExisting {
		t.Fatalf("second import = %v %v", outcome, err)
	}
	if _, err := ImportRun(root, importMeta("r", false)); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("different meta = %v, want ErrImportConflict", err)
	}
	got, _, _ := NewReader(root).Get("r")
	if got.CompletedAt == nil {
		t.Fatal("the conflicting import changed the existing folder")
	}
}

func TestImportRunTreatsADamagedExistingFolderAsAConflict(t *testing.T) {
	root := t.TempDir()
	dir := RunDir(root, "r")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFileName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportRun(root, importMeta("r", true)); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("damaged existing = %v, want ErrImportConflict", err)
	}
}

func TestImportRunRejectsAnIdThatIsNotBare(t *testing.T) {
	if _, err := ImportRun(t.TempDir(), importMeta("../x", true)); err == nil {
		t.Fatal("a path-like id was accepted")
	}
}

func TestScratchFoldersAreNotRunsAndCanBeRemoved(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(VersionedRunsDir(root), importTempPrefix+"abc")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, MetaFileName), []byte(`{"schemaVersion":1,"runId":"other"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	metas, skipped, err := NewReader(root).List()
	if err != nil || len(metas) != 0 || len(skipped) != 0 {
		t.Fatalf("List = %v %v %v", metas, skipped, err)
	}
	n, err := RemoveImportScratch(root)
	if err != nil || n != 1 {
		t.Fatalf("RemoveImportScratch = %d %v", n, err)
	}
	if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch survived: %v", err)
	}
}

func TestMigrationLockExcludesEditorsAndSharesAmongThem(t *testing.T) {
	root := t.TempDir()
	a, err := ShareMigration(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ShareMigration(root)
	if err != nil {
		t.Fatalf("second editor: %v", err)
	}
	_ = a.Release()
	_ = b.Release()

	m, err := LockMigration(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ShareMigration(root); !errors.Is(err, ErrMigrationInProgress) {
		t.Fatalf("editor during migration = %v, want ErrMigrationInProgress", err)
	}
	_ = m.Release()
	if err := m.Release(); err != nil {
		t.Fatalf("double release: %v", err)
	}
	c, err := ShareMigration(root)
	if err != nil {
		t.Fatalf("editor after migration: %v", err)
	}
	_ = c.Release()
}
