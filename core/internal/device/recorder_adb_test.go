package device

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

var androidDesc = Descriptor{ID: "SER1", Platform: PlatformAndroid, Kind: KindDevice, State: StateOnline}

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
