package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/process"
)

var androidDesc = Descriptor{ID: "SER1", Platform: PlatformAndroid, Kind: KindDevice, State: StateOnline}

var errPull = errors.New("device offline")

func startAndroid(t *testing.T, h *recorderHarness, pids ...int) (Recording, string) {
	t.Helper()
	h.stageAndroid(pids...)
	dir := t.TempDir()
	rec, err := adbRecorder{deps: h.deps(), wait: h.wait}.Start(context.Background(), androidDesc, dir, "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.awaitIdle()
	return rec, dir
}

func TestAndroidRecorder_StartClearsDirectoryThenStartsSegmentWithPidEcho(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)
	defer func() { _ = rec.Discard(context.Background()) }()

	clearAt := h.indexOf("adb -s SER1 shell rm -rf " + adbRecordingDir + " && mkdir -p " + adbRecordingDir)
	startAt := h.indexOf("start adb -s SER1 shell echo $$; exec screenrecord --bit-rate 4000000 " + adbRecordingDir + "/segment-000.mp4")
	if clearAt > startAt {
		t.Fatalf("the device directory was cleared after the first segment started")
	}
}

func TestAndroidRecorder_RotationStartsNextSegmentBeforeStoppingCurrent(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111, 222)
	defer func() { _ = rec.Discard(context.Background()) }()

	h.rotateOnce()

	next := h.indexOf("start", "segment-001.mp4")
	stop := h.indexOf("shell kill -INT 111")
	if next > stop {
		t.Fatalf("the next segment started after the current one was stopped")
	}
	if h.count("kill -INT 222") != 0 {
		t.Fatalf("the new segment was interrupted by the rotation")
	}
}

func TestAndroidRecorder_KeepsOnlyTheLastTwoSegments(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111, 222, 333, 444)

	h.rotateOnce()
	h.rotateOnce()
	h.rotateOnce()
	files, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{
		filepath.Join(dir, ".video-1-002.mp4"),
		filepath.Join(dir, ".video-1-003.mp4"),
	}
	if len(files) != 2 || files[0] != want[0] || files[1] != want[1] {
		t.Fatalf("files = %v, want %v", files, want)
	}
	if h.count("rm -f "+adbRecordingDir+"/segment-000.mp4") != 1 || h.count("rm -f "+adbRecordingDir+"/segment-001.mp4") != 1 {
		t.Fatalf("segments that fell out of the window were not removed from the device: %q", h.snapshot())
	}
	if h.count("adb -s SER1 pull") != 2 {
		t.Fatalf("pulled %d segments, want 2", h.count("adb -s SER1 pull"))
	}
}

func TestAndroidRecorder_StopInterruptsPullsThenClearsTheDevice(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111)

	files, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	local := filepath.Join(dir, ".video-1-000.mp4")
	if len(files) != 1 || files[0] != local {
		t.Fatalf("files = %v, want [%s]", files, local)
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatalf("the pulled file is missing: %v", err)
	}
	interrupt := h.indexOf("shell kill -INT 111")
	pull := h.indexOf("pull " + adbRecordingDir + "/segment-000.mp4 " + local)
	clear := h.find(pull, "shell rm -rf "+adbRecordingDir)
	if interrupt > pull || clear < 0 {
		t.Fatalf("expected interrupt, then pull, then clear, got %q", h.snapshot())
	}
}

func TestAndroidRecorder_DiscardClearsTheDeviceAndRemovesPulledFiles(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)
	files, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := rec.Discard(context.Background()); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if _, err := os.Stat(files[0]); !os.IsNotExist(err) {
		t.Fatalf("the pulled file survived Discard: %v", err)
	}
	if _, err := rec.Stop(context.Background()); err == nil {
		t.Fatal("Stop after Discard succeeded")
	}
}

func TestAndroidRecorder_DiscardOfARunningRecordingPullsNothing(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)

	if err := rec.Discard(context.Background()); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if h.count("pull") != 0 {
		t.Fatal("Discard pulled a segment")
	}
	h.indexOf("shell kill -INT 111")
	h.indexOf("shell rm -rf " + adbRecordingDir)
}

func TestAndroidRecorder_CancelledStopKillsAndStillCleansTheDevice(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := rec.Stop(ctx)

	if err == nil || files != nil {
		t.Fatalf("Stop = %v, %v, want an error and no files", files, err)
	}
	if !h.procs[0].Killed() {
		t.Fatal("the host adb process was not killed")
	}
	if h.count("pull") != 0 {
		t.Fatal("a cancelled Stop pulled a segment")
	}
	h.indexOf("shell rm -rf " + adbRecordingDir)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("local files left behind: %v", entries)
	}
}

func TestAndroidRecorder_PullFailureRemovesPartialFilesAndCleansTheDevice(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111, 222)
	h.rotateOnce()
	h.pullErr = errPull

	files, err := rec.Stop(context.Background())

	if err == nil || files != nil {
		t.Fatalf("Stop = %v, %v, want an error and no files", files, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("local files left behind: %v", entries)
	}
	if h.find(h.indexOf("pull"), "shell rm -rf "+adbRecordingDir) < 0 {
		t.Fatal("the device directory was not cleared after the failed pull")
	}
}

func TestAndroidRecorder_StopTwiceFails(t *testing.T) {
	h := newRecorderHarness(t)
	rec, _ := startAndroid(t, h, 111)
	if _, err := rec.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := rec.Stop(context.Background()); err == nil {
		t.Fatal("second Stop succeeded")
	}
}

func TestAndroidRecorder_StartFailsWhenTheSegmentReportsNoPid(t *testing.T) {
	h := newRecorderHarness(t)
	proc := process.NewFakeBackgroundProcess()
	proc.Exit(1)
	h.bg.Stub(proc)

	rec, err := adbRecorder{deps: h.deps(), wait: h.wait}.Start(context.Background(), androidDesc, t.TempDir(), "video-1")

	if err == nil || rec != nil {
		t.Fatalf("Start = %v, %v, want an error", rec, err)
	}
	h.indexOf("shell rm -rf " + adbRecordingDir)
}

func TestAndroidRecorder_FailedRotationKeepsTheLiveSegmentAndRetriesNextInterval(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111)
	h.bg.DefaultErr = errPull
	h.rotateOnce()
	h.bg.DefaultErr = nil
	h.stageAndroid(222)
	h.rotateOnce()

	files, err := rec.Stop(context.Background())

	if err != nil || len(files) != 2 {
		t.Fatalf("Stop = %v, %v, want both segments", files, err)
	}
	h.indexOf("start", "segment-001.mp4")
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("directory holds %d files, want 2: %v", len(entries), entries)
	}
}

func TestAndroidRecorder_SegmentThatNeverFinalizesIsDroppedAndTheRestIsKept(t *testing.T) {
	h := newRecorderHarness(t)
	h.stageAndroid(111, 222)
	rec, err := adbRecorder{deps: h.deps(), wait: h.wait, finalizeTimeout: boundTestDeadline}.Start(context.Background(), androidDesc, t.TempDir(), "video-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.awaitIdle()
	h.noFinalize.Store(true)
	h.rotateOnce()
	h.noFinalize.Store(false)

	files, err := rec.Stop(context.Background())

	if err != nil || len(files) != 1 {
		t.Fatalf("Stop = %v, %v, want the second segment only", files, err)
	}
	if h.count("pull", "segment-000.mp4") != 0 {
		t.Fatal("the unfinished segment was pulled")
	}
}

func TestAndroidRecorder_StopFailsWhenNoSegmentFinalized(t *testing.T) {
	h := newRecorderHarness(t)
	rec, dir := startAndroid(t, h, 111)
	h.noFinalize.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), boundTestDeadline)
	defer cancel()

	files, err := rec.Stop(ctx)

	if err == nil || files != nil {
		t.Fatalf("Stop = %v, %v, want an error", files, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("local files left behind: %v", entries)
	}
}
