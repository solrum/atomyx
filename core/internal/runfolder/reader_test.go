package runfolder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMeta(t *testing.T, root, id, body string) {
	t.Helper()
	dir := RunDir(root, id)
	if err := os.MkdirAll(ArtifactsDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ArtifactsDir(dir), "evidence.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MetaFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReaderGetNormalizesAnUnfinishedRun(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "r1", `{"schemaVersion":1,"runId":"r1","kind":"mcp","startedAt":5,"result":"passed","outcome":"passed"}`)

	meta, ok, err := NewReader(root).Get("r1")
	if err != nil || !ok {
		t.Fatalf("Get: ok %v err %v", ok, err)
	}
	if meta.Result != ResultCancelled || meta.Outcome != OutcomeInterrupted || meta.CompletedAt != nil {
		t.Fatalf("unfinished run: got result %q outcome %q", meta.Result, meta.Outcome)
	}
}

func TestReaderGetDerivesResultFromOutcome(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "r1", `{"schemaVersion":1,"runId":"r1","kind":"script","startedAt":5,"completedAt":9,"outcome":"deviceLost"}`)

	meta, _, err := NewReader(root).Get("r1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Result != ResultCancelled || meta.Outcome != OutcomeDeviceLost {
		t.Fatalf("got result %q outcome %q", meta.Result, meta.Outcome)
	}
}

func TestReaderGetRejectsContractViolations(t *testing.T) {
	cases := map[string]string{
		"future schema":       `{"schemaVersion":2,"runId":"r1","startedAt":1}`,
		"missing schema":      `{"runId":"r1","startedAt":1}`,
		"unknown outcome":     `{"schemaVersion":1,"runId":"r1","startedAt":1,"completedAt":2,"outcome":"exploded"}`,
		"completed no result": `{"schemaVersion":1,"runId":"r1","startedAt":1,"completedAt":2}`,
		"not json":            `{`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeMeta(t, root, "r1", body)
			if _, ok, err := NewReader(root).Get("r1"); err == nil || ok {
				t.Fatalf("got ok %v err %v, want an error", ok, err)
			}
		})
	}
}

func TestReaderGetReportsMissingAndUnsafeIDs(t *testing.T) {
	r := NewReader(t.TempDir())
	for _, id := range []string{"absent", "", "..", "a/b", `a\b`} {
		if _, ok, err := r.Get(id); ok || err != nil {
			t.Fatalf("Get(%q): ok %v err %v, want not found", id, ok, err)
		}
	}
}

func TestReaderListSkipsFoldersWithoutMetaAndReportsDamagedOnes(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "b-run", `{"schemaVersion":1,"runId":"b-run","kind":"script","startedAt":2,"completedAt":3,"result":"passed"}`)
	writeMeta(t, root, "a-run", `{"schemaVersion":1,"kind":"script","startedAt":1}`)
	writeMeta(t, root, "damaged", `{`)
	if err := os.MkdirAll(RunDir(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(VersionedRunsDir(root), "stray.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	metas, err := NewReader(root).List()
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("List error: got %v, want one naming the damaged run", err)
	}
	if len(metas) != 2 || metas[0].RunID != "a-run" || metas[1].RunID != "b-run" {
		t.Fatalf("List: got %+v, want a-run then b-run", metas)
	}
}

func TestReaderListOfAnEmptyRootIsEmpty(t *testing.T) {
	metas, err := NewReader(t.TempDir()).List()
	if err != nil || len(metas) != 0 {
		t.Fatalf("got %v, %v", metas, err)
	}
}

func TestRemoveRunDeletesTheFolderAndToleratesAbsence(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "r1", `{"schemaVersion":1,"runId":"r1","startedAt":1}`)

	if ok, err := RemoveRun(root, "r1"); !ok || err != nil {
		t.Fatalf("first Remove: ok %v err %v", ok, err)
	}
	if _, err := os.Stat(RunDir(root, "r1")); !os.IsNotExist(err) {
		t.Fatalf("folder survived: %v", err)
	}
	if ok, err := RemoveRun(root, "r1"); ok || err != nil {
		t.Fatalf("second Remove: ok %v err %v, want absent without error", ok, err)
	}
}
