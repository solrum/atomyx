package recording_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
)

const testStopBound = 7 * time.Second

type fakeRecorder struct {
	startErr  error
	stopErr   error
	segments  []string
	starts    int
	stops     int
	discards  int
	startName string
	stopCtx   ctxSeen
	discCtx   ctxSeen
	dir       string
}

func (f *fakeRecorder) Start(_ context.Context, _ device.Descriptor, dir, name string) (device.Recording, error) {
	f.starts++
	f.startName = name
	f.dir = dir
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &fakeRecording{f: f}, nil
}

// ctxSeen is what a fake observed about the context of one call, taken
// at call time because the caller cancels it afterwards.
type ctxSeen struct {
	called   bool
	err      error
	remains  time.Duration
	deadline bool
}

func seen(ctx context.Context) ctxSeen {
	deadline, ok := ctx.Deadline()
	return ctxSeen{called: true, err: ctx.Err(), remains: time.Until(deadline), deadline: ok}
}

type fakeRecording struct{ f *fakeRecorder }

func (r *fakeRecording) Stop(ctx context.Context) ([]string, error) {
	r.f.stops++
	r.f.stopCtx = seen(ctx)
	if r.f.stopErr != nil {
		return nil, r.f.stopErr
	}
	var paths []string
	for _, seg := range r.f.segments {
		p := filepath.Join(r.f.dir, seg)
		if err := os.WriteFile(p, []byte(seg), 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (r *fakeRecording) Discard(ctx context.Context) error {
	r.f.discards++
	r.f.discCtx = seen(ctx)
	return nil
}

type recordingLogger struct {
	logging.NoopLogger
	errors []error
}

func (l *recordingLogger) Error(_ string, err error, _ logging.LogContext) {
	l.errors = append(l.errors, err)
}

func newRun(t *testing.T) *runfolder.Writer {
	t.Helper()
	starter := runfolder.NewStarter(t.TempDir(), time.Now, bytes.NewReader([]byte{1, 2, 3, 4}))
	w, err := starter.Start("Login", runfolder.Meta{Kind: runfolder.KindScript})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func artifactNames(t *testing.T, w *runfolder.Writer) []string {
	t.Helper()
	entries, err := os.ReadDir(runfolder.ArtifactsDir(w.Dir()))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func lifecycle(rec device.ScreenRecorder, log logging.Logger) *recording.Lifecycle {
	return recording.NewLifecycle(rec, log, recording.WithStopBound(testStopBound))
}

func begin(t *testing.T, rec device.ScreenRecorder, log logging.Logger, w *runfolder.Writer) *recording.Capture {
	t.Helper()
	return lifecycle(rec, log).Begin(context.Background(), device.Descriptor{}, w, "login")
}

func TestOffMakesNoRecorderCall(t *testing.T) {
	w := newRun(t)
	capture := lifecycle(nil, logging.NoopLogger{}).Begin(context.Background(), device.Descriptor{}, w, "login")
	if got := capture.Finish(runfolder.ResultFailed); got != nil {
		t.Fatalf("Finish = %+v, want nil", got)
	}
	if names := artifactNames(t, w); names != nil {
		t.Fatalf("artifacts = %v", names)
	}
}

func TestFailedRunKeepsTheSegmentsUnderFinalNames(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{segments: []string{".video-login-a.mp4", ".video-login-b.mp4"}}
	capture := begin(t, rec, logging.NoopLogger{}, w)

	if rec.startName != runfolder.VideoNamePrefix+"login" {
		t.Fatalf("start name = %q", rec.startName)
	}
	got := capture.Finish(runfolder.ResultFailed)

	want := &runfolder.Recording{State: runfolder.RecordingKept, Files: []string{"video-login-1.mp4", "video-login-2.mp4"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Finish = %+v, want %+v", got, want)
	}
	if names := artifactNames(t, w); !reflect.DeepEqual(names, want.Files) {
		t.Fatalf("artifacts = %v", names)
	}
	if rec.discards != 0 {
		t.Fatalf("discards = %d, want 0", rec.discards)
	}
}

func TestPassedAndCancelledRunsDiscardWithoutStopping(t *testing.T) {
	for _, result := range []runfolder.Result{runfolder.ResultPassed, runfolder.ResultCancelled} {
		t.Run(string(result), func(t *testing.T) {
			w := newRun(t)
			rec := &fakeRecorder{segments: []string{".video-login-a.mp4"}}
			capture := begin(t, rec, logging.NoopLogger{}, w)

			if got := capture.Finish(result); got != nil {
				t.Fatalf("Finish = %+v, want nil", got)
			}
			if rec.stops != 0 || rec.discards != 1 {
				t.Fatalf("stops = %d, discards = %d", rec.stops, rec.discards)
			}
		})
	}
}

func TestCleanupContextOutlivesACancelledCaller(t *testing.T) {
	for _, result := range []runfolder.Result{runfolder.ResultFailed, runfolder.ResultPassed, runfolder.ResultCancelled} {
		t.Run(string(result), func(t *testing.T) {
			w := newRun(t)
			rec := &fakeRecorder{segments: []string{".video-login-a.mp4"}}
			ctx, cancel := context.WithCancel(context.Background())
			capture := lifecycle(rec, logging.NoopLogger{}).Begin(ctx, device.Descriptor{}, w, "login")
			cancel()

			capture.Finish(result)

			used := rec.stopCtx
			if result != runfolder.ResultFailed {
				used = rec.discCtx
			}
			if !used.called || used.err != nil {
				t.Fatalf("cleanup context = %+v, want a live context", used)
			}
			if !used.deadline || used.remains > testStopBound {
				t.Fatalf("cleanup context = %+v, want a deadline within %v", used, testStopBound)
			}
		})
	}
}

func TestUnsupportedDeviceIsReportedAndTheRunContinues(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{startErr: diag.NewRecordingUnsupported()}
	log := &recordingLogger{}
	capture := begin(t, rec, log, w)

	for _, result := range []runfolder.Result{runfolder.ResultFailed, runfolder.ResultPassed} {
		got := capture.Finish(result)
		want := &runfolder.Recording{State: runfolder.RecordingUnsupported, Code: diag.CodeRecordingUnsupported}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Finish(%s) = %+v, want %+v", result, got, want)
		}
	}
	if rec.stops != 0 || rec.discards != 0 || len(log.errors) != 0 {
		t.Fatalf("stops = %d, discards = %d, logged = %v", rec.stops, rec.discards, log.errors)
	}
}

func TestOtherStartFailureIsLoggedAndReportedAsError(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{startErr: diag.New("ADB_FAILED", "adb failed", "", nil)}
	log := &recordingLogger{}
	capture := begin(t, rec, log, w)

	got := capture.Finish(runfolder.ResultFailed)
	want := &runfolder.Recording{State: runfolder.RecordingError, Code: "ADB_FAILED"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Finish = %+v, want %+v", got, want)
	}
	if len(log.errors) != 1 {
		t.Fatalf("logged = %v", log.errors)
	}
}

func TestStopFailureIsLoggedDiscardedAndReportedAsError(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{stopErr: diag.New("PULL_FAILED", "pull failed", "", nil)}
	log := &recordingLogger{}
	capture := begin(t, rec, log, w)

	got := capture.Finish(runfolder.ResultFailed)
	want := &runfolder.Recording{State: runfolder.RecordingError, Code: "PULL_FAILED"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Finish = %+v, want %+v", got, want)
	}
	if len(log.errors) != 1 || rec.discards != 1 {
		t.Fatalf("logged = %v, discards = %d", log.errors, rec.discards)
	}
}

func TestAdoptFailureIsReportedAsErrorAndDiscards(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{segments: []string{".video-login-a.mp4"}}
	capture := begin(t, rec, logging.NoopLogger{}, w)
	dir := runfolder.ArtifactsDir(w.Dir())
	if err := os.WriteFile(filepath.Join(dir, "video-login-1.mp4"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := capture.Finish(runfolder.ResultFailed)
	if got == nil || got.State != runfolder.RecordingError || got.Code == "" || got.Files != nil {
		t.Fatalf("Finish = %+v", got)
	}
	if rec.discards != 1 {
		t.Fatalf("discards = %d, want 1", rec.discards)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{segments: []string{".video-login-a.mp4"}}
	capture := begin(t, rec, logging.NoopLogger{}, w)

	first := capture.Finish(runfolder.ResultFailed)
	second := capture.Finish(runfolder.ResultPassed)

	if !reflect.DeepEqual(first, second) || rec.stops != 1 || rec.discards != 0 {
		t.Fatalf("first = %+v, second = %+v, stops = %d, discards = %d", first, second, rec.stops, rec.discards)
	}
}

func TestDefaultStopBoundCoversAFullSegmentPull(t *testing.T) {
	if recording.DefaultStopBound < 15*time.Second {
		t.Fatalf("DefaultStopBound = %v, want at least 15s", recording.DefaultStopBound)
	}
}

func TestFinishAfterADiscardStaysDiscarded(t *testing.T) {
	w := newRun(t)
	rec := &fakeRecorder{segments: []string{".video-login-a.mp4"}}
	capture := begin(t, rec, logging.NoopLogger{}, w)

	capture.Finish(runfolder.ResultPassed)
	if got := capture.Finish(runfolder.ResultFailed); got != nil {
		t.Fatalf("second Finish = %+v, want nil", got)
	}
	if rec.stops != 0 || rec.discards != 1 {
		t.Fatalf("stops = %d, discards = %d", rec.stops, rec.discards)
	}
}
