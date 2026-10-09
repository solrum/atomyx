package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/storage"
)

const failingBareScript = "appId: com.example.app\nname: Broken\nsteps:\n  - assertVisible: \"Does Not Exist\"\n"

// fakeScreenRecorder stands in for the device recorder. Stop writes the
// configured segments into the recording directory.
type fakeScreenRecorder struct {
	segments       []string
	starts         int
	stops          int
	discards       int
	discardCtxErr  error
	discardHadDead bool
}

func (f *fakeScreenRecorder) Start(_ context.Context, _ device.Descriptor, dir, _ string) (device.Recording, error) {
	f.starts++
	return &fakeScreenRecording{f: f, dir: dir}, nil
}

type fakeScreenRecording struct {
	f   *fakeScreenRecorder
	dir string
}

func (r *fakeScreenRecording) Stop(context.Context) ([]string, error) {
	r.f.stops++
	var paths []string
	for _, seg := range r.f.segments {
		p := filepath.Join(r.dir, seg)
		if err := os.WriteFile(p, []byte(seg), 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (r *fakeScreenRecording) Discard(ctx context.Context) error {
	r.f.discards++
	r.f.discardCtxErr = ctx.Err()
	_, r.f.discardHadDead = ctx.Deadline()
	return nil
}

func videoServer(t *testing.T, rec *fakeScreenRecorder, onFailure bool) runServer {
	t.Helper()
	r := newRunServerWith(t, func(d *ServerDeps) {
		d.Recorder = rec
		d.VideoOnFailure = onFailure
	})
	selectMockDevice(t, r.cs, t.Context())
	return r
}

func TestRunScriptFailedOwnRunKeepsTheVideoAndListsIt(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4", ".tmp-b.mp4"}}
	r := videoServer(t, rec, true)

	got := r.call("run_script", map[string]any{"script": failingBareScript})

	meta := r.onlyMeta(t)
	artifacts := runfolder.ArtifactsDir(runfolder.RunDir(r.root, meta.RunID))
	wantNames := []string{"video-run-1.mp4", "video-run-2.mp4"}
	if meta.Recording == nil || meta.Recording.State != runfolder.RecordingKept || strings.Join(meta.Recording.Files, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("meta recording = %+v, want kept %v", meta.Recording, wantNames)
	}
	wire, _ := got["recording"].(map[string]any)
	files, _ := wire["files"].([]any)
	if wire["state"] != "kept" || len(files) != 2 {
		t.Fatalf("result recording = %v, want kept with two files", got["recording"])
	}
	for i, name := range wantNames {
		want := filepath.Join(artifacts, name)
		if files[i] != want {
			t.Errorf("files[%d] = %v, want %s", i, files[i], want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("kept file %s: %v", want, err)
		}
	}
}

func TestRunScriptPassedOwnRunKeepsNoVideo(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	r := videoServer(t, rec, true)

	got := r.call("run_script", map[string]any{"script": passingScreenshotScript})

	if got["ok"] != true {
		t.Fatalf("run_script = %v", got)
	}
	if _, present := got["recording"]; present {
		t.Errorf("a passed run lists a recording: %v", got["recording"])
	}
	if rec.starts != 1 || rec.discards != 1 || rec.stops != 0 {
		t.Errorf("starts=%d stops=%d discards=%d, want 1, 0, 1", rec.starts, rec.stops, rec.discards)
	}
	meta := r.onlyMeta(t)
	if meta.Recording != nil {
		t.Errorf("meta recording = %+v, want none", meta.Recording)
	}
	for _, name := range artifactFiles(t, runfolder.RunDir(r.root, meta.RunID)) {
		if strings.Contains(name, "video") {
			t.Errorf("a passed run kept %s", name)
		}
	}
}

func TestRunScriptWithVideoOffMakesNoRecorderCall(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	r := videoServer(t, rec, false)

	got := r.call("run_script", map[string]any{"script": failingBareScript})

	if rec.starts+rec.stops+rec.discards != 0 {
		t.Errorf("recorder called: starts=%d stops=%d discards=%d", rec.starts, rec.stops, rec.discards)
	}
	if _, present := got["recording"]; present {
		t.Errorf("result lists a recording with video off: %v", got["recording"])
	}
}

func TestRunScriptInsideAnOpenRunNeverRecords(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	r := videoServer(t, rec, true)
	r.call("start_run", map[string]any{"name": "explore"})

	got := r.call("run_script", map[string]any{"script": failingBareScript})
	r.call("finish_run", map[string]any{"summary": "done"})

	if rec.starts != 0 {
		t.Errorf("recorder started %d times for an open run, want 0", rec.starts)
	}
	if _, present := got["recording"]; present {
		t.Errorf("a script inside an open run lists a recording: %v", got["recording"])
	}
	if meta := r.onlyMeta(t); meta.Recording != nil {
		t.Errorf("open run meta recording = %+v, want none", meta.Recording)
	}
}

func TestOwnRunCanceledByTheCallDiscardsUnderAnUncanceledBoundedContext(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	root := t.TempDir()
	s := newRunSession(runSessionDeps{
		Store:      storage.NewRunStore(),
		Catalog:    testRunCatalog(storage.NewInMemoryStorage(), root),
		Starter:    testRunStarter(root),
		Clock:      orchestra.NewFakeClock(0),
		Logger:     logging.NoopLogger{},
		Recordings: recording.NewLifecycle(rec, logging.NoopLogger{}, recording.WithStopBound(time.Minute)),
	})
	ctx, cancel := context.WithCancel(context.Background())

	run, err := s.RunScript(ctx, "slow", &device.Descriptor{ID: "emulator-5554"}, func(scriptTarget) (runner.ScriptResult, error) {
		cancel()
		return runner.ScriptResult{}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if run.Recording != nil || rec.stops != 0 || rec.discards != 1 {
		t.Errorf("recording=%+v stops=%d discards=%d, want none, 0, 1", run.Recording, rec.stops, rec.discards)
	}
	if rec.discardCtxErr != nil || !rec.discardHadDead {
		t.Errorf("cleanup context err=%v bounded=%v, want live and bounded", rec.discardCtxErr, rec.discardHadDead)
	}
}

func budgetRecordingSession(rec *fakeScreenRecorder, root string) *runSession {
	return newRunSession(runSessionDeps{
		Store:      storage.NewRunStore(),
		Catalog:    testRunCatalog(storage.NewInMemoryStorage(), root),
		Starter:    testRunStarter(root),
		Clock:      orchestra.NewFakeClock(0),
		Logger:     logging.NoopLogger{},
		Recordings: recording.NewLifecycle(rec, logging.NoopLogger{}, recording.WithStopBound(time.Minute)),
	})
}

func TestOwnRunCanceledByTheToolBudgetKeepsTheVideoAndTheCancelledOutcome(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	root := t.TempDir()
	clk := clock.NewManualClock(0)
	budget, stop := newBudgetContext(context.Background(), clk, 30_000)
	defer stop()
	clk.AwaitSleepers(1)

	run, err := budgetRecordingSession(rec, root).RunScript(budget, "hang", &device.Descriptor{ID: "emulator-5554"}, func(target scriptTarget) (runner.ScriptResult, error) {
		clk.Advance(30_000)
		<-target.Ctx.Done()
		return runner.ScriptResult{}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if run.Recording == nil || run.Recording.State != runfolder.RecordingKept || rec.stops != 1 || rec.discards != 0 {
		t.Errorf("recording=%+v stops=%d discards=%d, want kept, 1, 0", run.Recording, rec.stops, rec.discards)
	}
	metas, _, readErr := runfolder.NewReader(root).List()
	if readErr != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v)", len(metas), readErr)
	}
	if metas[0].Outcome != runfolder.OutcomeCancelled || metas[0].Result != runfolder.ResultCancelled {
		t.Errorf("meta = %s/%s, want cancelled", metas[0].Result, metas[0].Outcome)
	}
}

func TestOwnRunCanceledByTheCallerWithinABudgetDiscardsTheVideo(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	clk := clock.NewManualClock(0)
	caller, cancelCaller := context.WithCancel(context.Background())
	budget, stop := newBudgetContext(caller, clk, 30_000)
	defer stop()
	clk.AwaitSleepers(1)

	run, err := budgetRecordingSession(rec, t.TempDir()).RunScript(budget, "slow", &device.Descriptor{ID: "emulator-5554"}, func(target scriptTarget) (runner.ScriptResult, error) {
		cancelCaller()
		<-target.Ctx.Done()
		return runner.ScriptResult{}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if run.Recording != nil || rec.stops != 0 || rec.discards != 1 {
		t.Errorf("recording=%+v stops=%d discards=%d, want none, 0, 1", run.Recording, rec.stops, rec.discards)
	}
}

func TestOwnRunThatLostItsDeviceWhenTheBudgetElapsedDiscardsTheVideo(t *testing.T) {
	rec := &fakeScreenRecorder{segments: []string{".tmp-a.mp4"}}
	clk := clock.NewManualClock(0)
	budget, stop := newBudgetContext(context.Background(), clk, 30_000)
	defer stop()
	clk.AwaitSleepers(1)

	run, err := budgetRecordingSession(rec, t.TempDir()).RunScript(budget, "lost", &device.Descriptor{ID: "emulator-5554"}, func(target scriptTarget) (runner.ScriptResult, error) {
		clk.Advance(30_000)
		<-target.Ctx.Done()
		return runner.ScriptResult{Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost}}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if run.Recording != nil || rec.stops != 0 || rec.discards != 1 {
		t.Errorf("recording=%+v stops=%d discards=%d, want none, 0, 1", run.Recording, rec.stops, rec.discards)
	}
}
