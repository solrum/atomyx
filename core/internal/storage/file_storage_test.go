package storage

import (
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/storageroot"
)

func newTestFileStorage(t *testing.T) *FileStorage {
	t.Helper()
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	return fs
}

func TestFileStorageSavesAndLoadsJSONRecord(t *testing.T) {
	fs := newTestFileStorage(t)
	type record struct {
		Title string   `json:"title"`
		Steps []string `json:"steps"`
	}
	want := record{Title: "x", Steps: []string{"a", "b"}}
	if err := fs.Save("bugs/123", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var got record
	found, err := fs.Load("bugs/123", &got)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load: expected key to be found")
	}
	if got.Title != want.Title || !equalStrings(got.Steps, want.Steps) {
		t.Fatalf("Load: got %+v, want %+v", got, want)
	}
}

func TestFileStorageJSONRecordOnDiskPathAndFormat(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if err := fs.Save("bugs/123", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, "bugs", "123.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	want := "{\n  \"v\": 1\n}"
	if string(body) != want {
		t.Fatalf("on-disk JSON body: got %q, want %q", body, want)
	}
}

func TestFileStorageSavesAndLoadsStringAsMarkdown(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if err := fs.Save("notes/plan", "# Plan\n\n- Step 1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, "notes", "plan.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	if string(body) != "# Plan\n\n- Step 1" {
		t.Fatalf("on-disk markdown body: got %q", body)
	}

	var loaded string
	found, err := fs.Load("notes/plan", &loaded)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || loaded != "# Plan\n\n- Step 1" {
		t.Fatalf("Load: got (%v, %q)", found, loaded)
	}
}

func TestFileStorageLoadReturnsNotFoundForMissingKey(t *testing.T) {
	fs := newTestFileStorage(t)
	var out map[string]any
	found, err := fs.Load("nope", &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("Load: expected key to be missing")
	}
}

func TestFileStorageListReturnsKeysUnderPrefix(t *testing.T) {
	fs := newTestFileStorage(t)
	for k, v := range map[string]int{"bugs/1": 1, "bugs/2": 2, "runs/a": 3} {
		if err := fs.Save(k, map[string]int{"x": v}); err != nil {
			t.Fatalf("Save(%q): %v", k, err)
		}
	}

	keys, err := fs.List("bugs")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"bugs/1", "bugs/2"}; !equalStrings(keys, want) {
		t.Fatalf("List: got %v, want %v", keys, want)
	}
}

func TestFileStorageListEmptyForMissingPrefix(t *testing.T) {
	fs := newTestFileStorage(t)
	keys, err := fs.List("nothing")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("List: got %v, want empty", keys)
	}
}

func TestFileStorageDeleteRemovesRecord(t *testing.T) {
	fs := newTestFileStorage(t)
	if err := fs.Save("bugs/1", map[string]int{"x": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := fs.Delete("bugs/1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var out map[string]int
	found, err := fs.Load("bugs/1", &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("Load: expected key to be deleted")
	}
}

func TestFileStorageDeleteRemovesBothSiblingExtensions(t *testing.T) {
	fs := newTestFileStorage(t)
	if err := fs.Save("k", "text body"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := fs.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var out string
	found, err := fs.Load("k", &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("Load: expected markdown record to be deleted")
	}
}

func TestFileStorageDeleteOfMissingKeyIsNotAnError(t *testing.T) {
	fs := newTestFileStorage(t)
	if err := fs.Delete("nope"); err != nil {
		t.Fatalf("Delete of missing key returned an error: %v", err)
	}
}

func TestFileStoragePathTraversalIsNeutralized(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if err := fs.Save("../escape", map[string]int{"x": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.json")); err == nil {
		t.Fatal("Save escaped the storage root")
	}
	if _, err := os.Stat(filepath.Join(root, "_", "escape.json")); err != nil {
		t.Fatalf("expected neutralized path under root: %v", err)
	}
}

func TestFileStorageSaveIsAtomicNoTempFileLeftBehind(t *testing.T) {
	fs := newTestFileStorage(t)
	if err := fs.Save("bugs/123", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(fs.root, "bugs"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "123.json" {
		t.Fatalf("directory entries after Save: got %v, want only 123.json", entries)
	}
}

func TestFileStorageSavePreservesExistingFilePermissions(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if err := fs.Save("bugs/123", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, "bugs", "123.json")
	const customPerm = 0o640
	if err := os.Chmod(path, customPerm); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	if err := fs.Save("bugs/123", map[string]int{"v": 2}); err != nil {
		t.Fatalf("Save (overwrite): %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != customPerm {
		t.Fatalf("permissions after overwrite: got %o, want %o", got, customPerm)
	}
}

func TestFileStorageSaveNewFileUsesDefaultPermissions(t *testing.T) {
	fs := newTestFileStorage(t)
	if err := fs.Save("bugs/123", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(fs.root, "bugs", "123.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != fileModeFile {
		t.Fatalf("permissions for new file: got %o, want %o", got, fileModeFile)
	}
}

func TestFileStorageSaveFailureLeavesOriginalFileIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory write-permission checks are bypassed for root")
	}

	root := t.TempDir()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if err := fs.Save("bugs/123", map[string]int{"v": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir := filepath.Join(root, "bugs")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, fileModeDir) })

	if err := fs.Save("bugs/123", map[string]int{"v": 2}); err == nil {
		t.Fatal("Save: expected an error when the directory rejects new files, got nil")
	}

	if err := os.Chmod(dir, fileModeDir); err != nil {
		t.Fatalf("Chmod dir (restore): %v", err)
	}
	var got map[string]int
	found, err := fs.Load("bugs/123", &got)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found || got["v"] != 1 {
		t.Fatalf("Load after failed Save: got (%v, %v), want the original record (v=1) untouched", found, got)
	}
}

func TestFileStorageResolvesRootFromEnvThenHome(t *testing.T) {
	envRoot := t.TempDir()
	t.Setenv(storageroot.EnvVar, envRoot)
	fs, err := NewFileStorage(FileStorageOptions{})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	if fs.root != envRoot {
		t.Fatalf("root: got %q, want %q", fs.root, envRoot)
	}

	t.Setenv(storageroot.EnvVar, "")
	fs, err = NewFileStorage(FileStorageOptions{})
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if want := filepath.Join(home, storageroot.HomeDirName); fs.root != want {
		t.Fatalf("root: got %q, want %q", fs.root, want)
	}
}

func TestFileStorageListChildrenIgnoresNestedRecords(t *testing.T) {
	fs, err := NewFileStorage(FileStorageOptions{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runs/a", "runs/b", "runs/v1/x/meta", "other/c"} {
		if err := fs.Save(key, map[string]string{"k": key}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := fs.ListChildren("runs")
	if err != nil || len(got) != 2 || got[0] != "runs/a" || got[1] != "runs/b" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := fs.ListChildren("absent"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("absent prefix: %v %v", got, err)
	}
}
