package runfolder

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 2, 7, 3, 9, 0, time.UTC)

func newTestStarter(root string, suffixes ...byte) *Starter {
	return NewStarter(root, func() time.Time { return testNow }, bytes.NewReader(suffixes))
}

func startOne(t *testing.T, root string) *Writer {
	t.Helper()
	w, err := newTestStarter(root, 0xab, 0xcd).Start("Login", Meta{Kind: KindScript, ScriptName: "login"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestStartCreatesTheFolderWithAnUnfinishedMetaAndALock(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)

	if w.ID() != "20261002-070309-login-abcd" {
		t.Fatalf("id = %q", w.ID())
	}
	if w.Dir() != RunDir(root, w.ID()) {
		t.Fatalf("dir = %q", w.Dir())
	}
	meta, ok, err := NewReader(root).Get(w.ID())
	if err != nil || !ok {
		t.Fatalf("read back: ok=%v err=%v", ok, err)
	}
	if meta.StartedAt != testNow.UnixMilli() || meta.CompletedAt != nil || meta.Result != "" || meta.Outcome != "" {
		t.Fatalf("start meta = %+v", meta)
	}
	if meta.Kind != KindScript || meta.ScriptName != "login" || meta.SchemaVersion != SchemaVersion {
		t.Fatalf("start meta lost its fields: %+v", meta)
	}
	if got := dirNames(t, w.Dir()); strings.Join(got, ",") != MetaFileName+","+LockFileName {
		t.Fatalf("folder holds %v", got)
	}
}

func TestStartClearsFinishedFieldsOfTheBaseMeta(t *testing.T) {
	done := int64(5)
	base := Meta{Kind: KindMCP, CompletedAt: &done, Result: ResultPassed, Outcome: OutcomePassed, RunID: "other", StartedAt: 1}
	root := t.TempDir()
	w, err := newTestStarter(root, 1, 2).Start("", base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.ID() != "20261002-070309-mcp-0102" {
		t.Fatalf("id = %q", w.ID())
	}
	meta, _, _ := NewReader(root).Get(w.ID())
	if meta.CompletedAt != nil || meta.Outcome != "" || meta.RunID != w.ID() || meta.StartedAt != testNow.UnixMilli() {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestStartRedrawsTheSuffixWhenTheFolderExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(RunDir(root, "20261002-070309-login-abcd"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := newTestStarter(root, 0xab, 0xcd, 0xab, 0xcd, 0x12, 0x34).Start("login", Meta{Kind: KindScript})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if w.ID() != "20261002-070309-login-1234" {
		t.Fatalf("id = %q", w.ID())
	}
}

func TestStartGivesUpAfterTheBoundedNumberOfCollisions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(RunDir(root, "20261002-070309-login-abcd"), 0o755); err != nil {
		t.Fatal(err)
	}
	same := bytes.Repeat([]byte{0xab, 0xcd}, MaxIDAttempts)
	_, err := newTestStarter(root, same...).Start("login", Meta{Kind: KindScript})
	if !errors.Is(err, ErrIDExhausted) {
		t.Fatalf("err = %v, want ErrIDExhausted", err)
	}
}

func TestStartReportsARandomSourceFailure(t *testing.T) {
	if _, err := newTestStarter(t.TempDir()).Start("x", Meta{Kind: KindScript}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFinalizeWritesTheVerdictAtomicallyAndReleasesTheLock(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	live := NewLockLiveness(root)
	if !live.IsLive(w.ID()) {
		t.Fatal("open writer is not live")
	}
	step := 2
	err := w.Finalize(OutcomeError, func(m *Meta) {
		m.FailedAtStep, m.TotalSteps = &step, 5
		m.RunID, m.StartedAt, m.Result = "tampered", 1, ResultPassed
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := NewReader(root).Get(w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Result != ResultFailed || meta.Outcome != OutcomeError || meta.CompletedAt == nil || *meta.CompletedAt != testNow.UnixMilli() {
		t.Fatalf("verdict = %+v", meta)
	}
	if meta.RunID != w.ID() || meta.StartedAt != testNow.UnixMilli() || *meta.FailedAtStep != 2 || meta.TotalSteps != 5 {
		t.Fatalf("meta = %+v", meta)
	}
	if live.IsLive(w.ID()) {
		t.Fatal("finalized run still live")
	}
	for _, name := range dirNames(t, w.Dir()) {
		if strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temporary file %s left behind", name)
		}
	}
}

func TestFinalizeTwiceIsAnErrorAndChangesNothing(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(w.Dir(), MetaFileName))
	if err := w.Finalize(OutcomeFailed, nil); !errors.Is(err, ErrFinalized) {
		t.Fatalf("second finalize = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(w.Dir(), MetaFileName))
	if !bytes.Equal(before, after) {
		t.Fatal("second finalize rewrote meta.json")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close after finalize = %v", err)
	}
}

func TestFinalizeRejectsAnUnknownOutcomeAndStaysOpen(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	if err := w.Finalize(Outcome("bogus"), nil); err == nil {
		t.Fatal("expected an error")
	}
	if !NewLockLiveness(root).IsLive(w.ID()) {
		t.Fatal("writer lost its lock after a rejected finalize")
	}
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatalf("finalize after rejection: %v", err)
	}
}

func TestCloseWithoutFinalizeReadsAsInterrupted(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	live := NewLockLiveness(root)
	meta, _, _ := NewReader(root).Get(w.ID())
	if got := meta.Settle(live); got.Outcome != "" {
		t.Fatalf("live run settled as %+v", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
	got := meta.Settle(live)
	if got.Result != ResultCancelled || got.Outcome != OutcomeInterrupted {
		t.Fatalf("crashed run settled as %+v", got)
	}
	if err := w.AppendLog([]byte(`{}`)); !errors.Is(err, ErrClosed) {
		t.Fatalf("append after close = %v", err)
	}
	if err := w.Finalize(OutcomePassed, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("finalize after close = %v", err)
	}
}

func TestLockLivenessIgnoresMissingRunsAndUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	live := NewLockLiveness(root)
	for _, id := range []string{"nope", "", "../x"} {
		if live.IsLive(id) {
			t.Fatalf("%q reported live", id)
		}
	}
}

func TestLockLivenessNeverCreatesALockFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(RunDir(root, "r1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if NewLockLiveness(root).IsLive("r1") {
		t.Fatal("run without a lock file reported live")
	}
	if names := dirNames(t, RunDir(root, "r1")); len(names) != 0 {
		t.Fatalf("probe left %v", names)
	}
}

func TestAppendLogStopsAtTheBoundAndRecordsOneMarker(t *testing.T) {
	w := startOne(t, t.TempDir())
	w.maxLogBytes = 20
	lines := []string{`{"a":1}`, `{"b":2}`, `{"c":3}`, `{"d":4}`}
	for _, l := range lines {
		if err := w.AppendLog([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	if !w.LogTruncated() {
		t.Fatal("bound was not reported")
	}
	body, err := os.ReadFile(filepath.Join(w.Dir(), LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(got) != 3 || got[0] != lines[0] || got[1] != lines[1] {
		t.Fatalf("log = %q", got)
	}
	var marker struct {
		Truncated  bool `json:"truncated"`
		LimitBytes int  `json:"limitBytes"`
	}
	if err := json.Unmarshal([]byte(got[2]), &marker); err != nil || !marker.Truncated || marker.LimitBytes != 20 {
		t.Fatalf("marker = %q, %v", got[2], err)
	}
}

func TestAppendLogRejectsAnEmbeddedNewline(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.AppendLog([]byte("a\nb")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestArtifactsDirIsCreatedOnDemand(t *testing.T) {
	w := startOne(t, t.TempDir())
	dir, err := w.ArtifactsDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != ArtifactsDir(w.Dir()) {
		t.Fatalf("dir = %q", dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("artifacts dir missing: %v", err)
	}
}

func TestUpdateRewritesTheLiveMetaAndKeepsTheRunOpen(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	summary := "mid run"

	err := w.Update(func(m *Meta) {
		m.MCP = &MCPInfo{Name: "n", Summary: &summary}
		m.RunID, m.StartedAt = "other", 1
		done := int64(9)
		m.CompletedAt, m.Result, m.Outcome = &done, ResultPassed, OutcomePassed
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, ok, err := NewReader(root).Get(w.ID())
	if err != nil || !ok {
		t.Fatalf("read back: ok=%v err=%v", ok, err)
	}
	if meta.MCP == nil || meta.MCP.Summary == nil || *meta.MCP.Summary != summary {
		t.Fatalf("mcp block = %+v", meta.MCP)
	}
	if meta.RunID != w.ID() || meta.StartedAt != testNow.UnixMilli() || meta.CompletedAt != nil || meta.Outcome != "" {
		t.Fatalf("update changed a protected field: %+v", meta)
	}
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatalf("finalize after update: %v", err)
	}
	if err := w.Update(func(*Meta) {}); !errors.Is(err, ErrFinalized) {
		t.Fatalf("update after finalize: %v", err)
	}
}

func TestSetSummaryRewritesAFinishedRunAndRefusesLiveOrSummarylessRuns(t *testing.T) {
	root := t.TempDir()
	w, err := newTestStarter(root, 0xab, 0xcd).Start("m", Meta{Kind: KindMCP, MCP: &MCPInfo{Name: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if ok, err := SetSummary(root, w.ID(), "x"); !ok || !errors.Is(err, ErrRunLive) {
		t.Fatalf("live run: ok=%v err=%v", ok, err)
	}
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := SetSummary(root, w.ID(), "after"); !ok || err != nil {
		t.Fatalf("finished run: ok=%v err=%v", ok, err)
	}
	meta, _, _ := NewReader(root).Get(w.ID())
	if meta.MCP.Name != "keep" || meta.MCP.Summary == nil || *meta.MCP.Summary != "after" || meta.Outcome != OutcomePassed {
		t.Fatalf("meta after SetSummary = %+v", meta)
	}
	if ok, err := SetSummary(root, "missing", "x"); ok || err != nil {
		t.Fatalf("missing run: ok=%v err=%v", ok, err)
	}

	script, err := newTestStarter(root, 0x01, 0x02).Start("s", Meta{Kind: KindScript})
	if err != nil {
		t.Fatal(err)
	}
	_ = script.Finalize(OutcomePassed, nil)
	if ok, err := SetSummary(root, script.ID(), "x"); !ok || !errors.Is(err, ErrNoSummary) {
		t.Fatalf("script run: ok=%v err=%v", ok, err)
	}
}

func TestStartPublishesTheLockFileOnlyOnceItIsLocked(t *testing.T) {
	root := t.TempDir()
	var stages []lockBuildStage
	lockBuildHook = func(stage lockBuildStage, dir string) {
		stages = append(stages, stage)
		if _, err := os.Stat(LockPath(dir)); !os.IsNotExist(err) {
			t.Errorf("stage %d: run.lock visible before it is locked: %v", stage, err)
		}
		id := filepath.Base(dir)
		if NewLockLiveness(root).IsLive(id) {
			t.Errorf("stage %d: starting run reported live", stage)
		}
		if _, err := os.Stat(LockPath(dir)); !os.IsNotExist(err) {
			t.Errorf("stage %d: probe created run.lock: %v", stage, err)
		}
	}
	t.Cleanup(func() { lockBuildHook = nil })

	w := startOne(t, root)

	if len(stages) != 2 || stages[0] != lockStageCreated || stages[1] != lockStageLocked {
		t.Fatalf("hook stages = %v", stages)
	}
	if !NewLockLiveness(root).IsLive(w.ID()) {
		t.Fatal("started run is not live")
	}
	for _, name := range dirNames(t, w.Dir()) {
		if strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temporary file left behind: %s", name)
		}
	}
}

func TestLockLivenessSeesAWriterOfTheSameProcess(t *testing.T) {
	root := t.TempDir()
	w := startOne(t, root)
	live := NewLockLiveness(root)
	if !live.IsLive(w.ID()) {
		t.Fatal("writer in the same process not reported live")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if live.IsLive(w.ID()) {
		t.Fatal("closed writer still reported live")
	}
}
