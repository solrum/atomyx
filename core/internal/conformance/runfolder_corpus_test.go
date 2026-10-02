package conformance

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
)

func writeCorpusRun(t *testing.T, root string) *runfolder.Writer {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 2, 7, 3, 9, 0, time.UTC) }
	starter := runfolder.NewStarter(root, now, bytes.NewReader([]byte{0xab, 0xcd}))
	w, err := starter.Start("Corpus Run", runfolder.Meta{Kind: runfolder.KindScript, ScriptName: "corpus"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.AppendLog([]byte(`{"event":"start"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ArtifactsDir(); err != nil {
		t.Fatal(err)
	}
	return w
}

func runTree(t *testing.T, root string) []string {
	t.Helper()
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func TestRunfolderWriterTreeShape(t *testing.T) {
	root := t.TempDir()
	w := writeCorpusRun(t, root)
	if err := w.Finalize(runfolder.OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(runTree(t, root))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "runfolder/written-run-tree.json", got, DiffJSON)
}

func TestRunfolderReaderReadsWhatTheWriterWrote(t *testing.T) {
	root := t.TempDir()
	w := writeCorpusRun(t, root)
	reader := runfolder.NewReader(root)
	live := runfolder.NewLockLiveness(root)

	meta, ok, err := reader.Get(w.ID())
	if err != nil || !ok {
		t.Fatalf("read open run: ok=%v err=%v", ok, err)
	}
	if got := meta.Settle(live); got.Result != "" || got.Outcome != "" {
		t.Fatalf("held lock settled as %+v, want running", got)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := meta.Settle(live); got.Result != runfolder.ResultCancelled || got.Outcome != runfolder.OutcomeInterrupted {
		t.Fatalf("released lock without finalize settled as %+v, want interrupted", got)
	}

	done := writeCorpusRunAt(t, root)
	if err := done.Finalize(runfolder.OutcomeFailed, nil); err != nil {
		t.Fatal(err)
	}
	finished, ok, err := reader.Get(done.ID())
	if err != nil || !ok {
		t.Fatalf("read finalized run: ok=%v err=%v", ok, err)
	}
	if got := finished.Settle(live); got.Result != runfolder.ResultFailed || got.Outcome != runfolder.OutcomeFailed {
		t.Fatalf("finalized run settled as %+v", got)
	}
	metas, skipped, err := reader.List()
	if err != nil || len(skipped) != 0 || len(metas) != 2 {
		t.Fatalf("list = %d metas, %v skipped, err %v", len(metas), skipped, err)
	}
}

func writeCorpusRunAt(t *testing.T, root string) *runfolder.Writer {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 2, 7, 3, 10, 0, time.UTC) }
	w, err := runfolder.NewStarter(root, now, bytes.NewReader([]byte{0x01, 0x02})).Start("second", runfolder.Meta{Kind: runfolder.KindSuite})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}
