package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

var simulatorDesc = Descriptor{ID: "UDID-1", Platform: PlatformIOS, Kind: KindSimulator, State: StateOnline}

func startSimulator(t *testing.T, h *recorderHarness, segments int) (Recording, string) {
	t.Helper()
	h.stageSimulator(segments)
	dir := t.TempDir()
	rec, err := simulatorRecorder{deps: h.deps(), wait: h.wait}.Start(context.Background(), simulatorDesc, dir, "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.awaitIdle()
	return rec, dir
}

func TestSimulatorRecorder_StartRunsSimctlRecordVideoIntoATemporaryFile(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	defer func() { _ = rec.Discard(context.Background()) }()

	h.indexOf("start xcrun simctl io UDID-1 recordVideo --codec hevc --force " + filepath.Join(dir, ".video-1-000.mp4"))
}

func TestSimulatorRecorder_RotationStopsTheCurrentSegmentBeforeStartingTheNext(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startSimulator(t, h, 2)
	defer func() { _ = rec.Discard(context.Background()) }()

	h.rotateOnce()

	if !h.procs[0].Interrupted() {
		t.Fatal("the current segment was not interrupted")
	}
	if h.count("recordVideo") != 2 {
		t.Fatalf("started %d recorders, want 2", h.count("recordVideo"))
	}
	if h.procs[1].Interrupted() {
		t.Fatal("the new segment was interrupted by the rotation")
	}
}

func TestSimulatorRecorder_KeepsOnlyTheLastTwoSegments(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 4)

	h.rotateOnce()
	h.rotateOnce()
	h.rotateOnce()
	files, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{filepath.Join(dir, ".video-1-002.mp4"), filepath.Join(dir, ".video-1-003.mp4")}
	if len(files) != 2 || files[0] != want[0] || files[1] != want[1] {
		t.Fatalf("files = %v, want %v", files, want)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("directory holds %d files, want 2: %v", len(entries), entries)
	}
}

func TestSimulatorRecorder_StopInterruptsTheRecorderAndReturnsTheFile(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)

	files, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if !h.procs[0].Interrupted() || h.procs[0].Killed() {
		t.Fatalf("interrupted = %v, killed = %v, want interrupted only", h.procs[0].Interrupted(), h.procs[0].Killed())
	}
	if len(files) != 1 || files[0] != filepath.Join(dir, ".video-1-000.mp4") {
		t.Fatalf("files = %v", files)
	}
	if _, err := rec.Stop(context.Background()); err == nil {
		t.Fatal("second Stop succeeded")
	}
}

func TestSimulatorRecorder_DiscardRemovesEveryFileEvenAfterStop(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	if _, err := rec.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := rec.Discard(context.Background()); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestSimulatorRecorder_DiscardOfARunningRecordingEndsItAndRemovesTheFile(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)

	if err := rec.Discard(context.Background()); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if !h.procs[0].Killed() || h.procs[0].Interrupted() {
		t.Fatalf("killed = %v, interrupted = %v, want killed only", h.procs[0].Killed(), h.procs[0].Interrupted())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestSimulatorRecorder_CancelledStopKillsTheRecorderAndRemovesTheFiles(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := rec.Stop(ctx)

	if err == nil || files != nil {
		t.Fatalf("Stop = %v, %v, want an error and no files", files, err)
	}
	if !h.procs[0].Killed() || h.procs[0].Interrupted() {
		t.Fatalf("killed = %v, interrupted = %v, want killed only", h.procs[0].Killed(), h.procs[0].Interrupted())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestSimulatorRecorder_RecorderThatFailedEarlyIsReportedAtStop(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	h.procs[0].ExitWithStderr(1, "Unable to boot")
	<-rec.(*simulatorRecording).segments[0].done

	files, err := rec.Stop(context.Background())

	if err == nil || files != nil {
		t.Fatalf("Stop = %v, %v, want the recorder failure", files, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestScreenRecorder_SimulatorUsesSimctl(t *testing.T) {
	h := newRecorderHarness(t)
	h.stageSimulator(1)

	rec, err := NewScreenRecorder(h.deps()).Start(context.Background(), simulatorDesc, t.TempDir(), "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	h.indexOf("start xcrun simctl io UDID-1 recordVideo")
}

func TestScreenRecorder_AndroidUsesAdb(t *testing.T) {
	h := newRecorderHarness(t)
	h.stageAndroid(111)

	rec, err := NewScreenRecorder(h.deps()).Start(context.Background(), androidDesc, t.TempDir(), "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()

	h.indexOf("start adb -s SER1 shell")
}

func TestSimulatorRecorder_SegmentThatIgnoredTheInterruptIsDroppedAtRotation(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 2)
	h.procs[0].IgnoreInterrupt()
	h.rotateOnce()

	files, err := rec.Stop(context.Background())

	want := []string{filepath.Join(dir, ".video-1-001.mp4")}
	if err != nil || len(files) != 1 || files[0] != want[0] {
		t.Fatalf("Stop = %v, %v, want %v", files, err, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %d files, want 1: %v", len(entries), entries)
	}
}

func TestSimulatorRecorder_StopFailsWhenTheOnlySegmentIgnoredTheInterrupt(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	h.procs[0].IgnoreInterrupt()

	files, err := rec.Stop(context.Background())

	if !errors.Is(err, process.ErrInterruptEscalated) || files != nil {
		t.Fatalf("Stop = %v, %v, want an escalated interrupt error", files, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestSimulatorRecorder_FailedRotationKeepsTheSegmentAndRetriesNextInterval(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startSimulator(t, h, 1)
	h.bg.DefaultErr = errPull
	h.rotateOnce()
	h.bg.DefaultErr = nil
	h.stageSimulator(1)
	h.rotateOnce()

	files, err := rec.Stop(context.Background())

	if err != nil || len(files) != 2 {
		t.Fatalf("Stop = %v, %v, want both segments", files, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("directory holds %d files, want 2: %v", len(entries), entries)
	}
}

type warnCountingLogger struct {
	logging.NoopLogger
	warnings atomic.Int32
}

func (l *warnCountingLogger) Warn(string, logging.LogContext) { l.warnings.Add(1) }

func TestSimulatorRecorder_LogsAFailedRotation(t *testing.T) {
	h := newRecorderHarness(t)
	logger := &warnCountingLogger{}
	h.stageSimulator(1)
	deps := h.deps()
	deps.Logger = logger
	rec, err := simulatorRecorder{deps: deps, wait: h.wait}.Start(context.Background(), simulatorDesc, t.TempDir(), "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rec.Discard(context.Background()) }()
	h.awaitIdle()
	h.bg.DefaultErr = errPull
	h.rotateOnce()

	if logger.warnings.Load() != 1 {
		t.Fatalf("logged %d warnings, want 1", logger.warnings.Load())
	}
}
