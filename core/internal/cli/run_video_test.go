package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shutdown"
)

const (
	failingScript = "appId: com.not.installed\n---\n- sleep: 1\n- launchApp\n"
	passingScript = "- sleep: 1\n"
	// slowFailingScript sleeps long enough for a stop signal to land while
	// the run is in flight, then fails.
	slowFailingScript = "appId: com.not.installed\n---\n- sleep: 800\n- launchApp\n"
	interruptDelay    = 100 * time.Millisecond
)

// fakeVideoRecorder stands in for the device recorder. Every Stop writes
// the configured segments into the recording directory.
type fakeVideoRecorder struct {
	segments []string
	onStart  func()
	starts   int
	stops    int
	discards int
	names    []string
}

func (f *fakeVideoRecorder) Start(_ context.Context, _ device.Descriptor, dir, name string) (device.Recording, error) {
	f.starts++
	f.names = append(f.names, name)
	if f.onStart != nil {
		f.onStart()
	}
	return &fakeVideoRecording{f: f, dir: dir}, nil
}

type fakeVideoRecording struct {
	f   *fakeVideoRecorder
	dir string
}

func (r *fakeVideoRecording) Stop(context.Context) ([]string, error) {
	r.f.stops++
	paths := make([]string, 0, len(r.f.segments))
	for _, seg := range r.f.segments {
		p := filepath.Join(r.dir, seg)
		if err := os.WriteFile(p, []byte(seg), 0o600); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (r *fakeVideoRecording) Discard(context.Context) error {
	r.f.discards++
	return nil
}

type videoRun struct {
	code      int
	stdout    string
	runDir    string
	artifacts string
}

func (r videoRun) meta(t *testing.T) runfolder.Meta {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.runDir, runfolder.MetaFileName))
	if err != nil {
		t.Fatal(err)
	}
	var m runfolder.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func runWithRecorder(t *testing.T, ctx context.Context, rec device.ScreenRecorder, text string, extra map[string]FlagValue) videoRun {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "flow.yml")
	if err := os.WriteFile(scriptPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	reportDir := t.TempDir()
	flags := map[string]FlagValue{"--file": scriptPath, "--report-dir": reportDir, "--report": "html,json", "--json": true}
	for k, v := range extra {
		flags[k] = v
	}
	var stdout, stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &stdout, Stderr: &stderr, Recorder: rec}
	code := RunScript(ctx, deps, flags)
	dir := singleRunDir(t, reportDir)
	return videoRun{code: code, stdout: stdout.String(), runDir: dir, artifacts: runfolder.ArtifactsDir(dir)}
}

func TestRunVideoFailedRunKeepsAndNamesTheFiles(t *testing.T) {
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4", ".tmp-b.mp4"}}
	run := runWithRecorder(t, context.Background(), rec, failingScript, map[string]FlagValue{"--video": true})

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1", run.code)
	}
	want := []string{"video-run-1.mp4", "video-run-2.mp4"}
	for _, name := range want {
		if _, err := os.Stat(filepath.Join(run.artifacts, name)); err != nil {
			t.Errorf("kept file %s is missing: %v", name, err)
		}
	}
	got := run.meta(t).Recording
	if got == nil || got.State != runfolder.RecordingKept || strings.Join(got.Files, ",") != strings.Join(want, ",") {
		t.Errorf("meta recording = %+v, want kept %v", got, want)
	}
	var payload struct {
		Recording *jsonRecording `json:"recording"`
	}
	if err := json.Unmarshal([]byte(run.stdout), &payload); err != nil {
		t.Fatalf("--json output: %v\n%s", err, run.stdout)
	}
	if payload.Recording == nil || payload.Recording.State != "kept" || strings.Join(payload.Recording.Files, ",") != strings.Join(want, ",") {
		t.Errorf("--json recording = %+v", payload.Recording)
	}
	reportJSON, err := os.ReadFile(filepath.Join(run.runDir, "report", "report.json"))
	if err != nil || !strings.Contains(string(reportJSON), "video-run-2.mp4") {
		t.Errorf("report.json does not list the files (%v):\n%s", err, reportJSON)
	}
	html, err := os.ReadFile(filepath.Join(run.runDir, "report", "report.html"))
	if err != nil || !strings.Contains(string(html), `href="../artifacts/video-run-1.mp4"`) {
		t.Errorf("report.html does not link the video (%v)", err)
	}
	if rec.discards != 0 {
		t.Errorf("a kept video was discarded %d times", rec.discards)
	}
}

func TestRunVideoPassedRunKeepsNone(t *testing.T) {
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	run := runWithRecorder(t, context.Background(), rec, passingScript, map[string]FlagValue{"--video": true})

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0", run.code)
	}
	if rec.starts != 1 || rec.discards != 1 || rec.stops != 0 {
		t.Errorf("starts=%d stops=%d discards=%d, want 1, 0, 1", rec.starts, rec.stops, rec.discards)
	}
	if run.meta(t).Recording != nil {
		t.Errorf("meta recording = %+v, want none", run.meta(t).Recording)
	}
	if strings.Contains(run.stdout, `"recording"`) {
		t.Errorf("--json mentions a recording:\n%s", run.stdout)
	}
	for _, name := range artifactNames(t, run.artifacts) {
		if strings.Contains(name, "video") {
			t.Errorf("passed run kept %s", name)
		}
	}
}

func TestRunVideoInterruptedRunDiscards(t *testing.T) {
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	ctx, cancel := context.WithCancelCause(context.Background())
	rec.onStart = func() {
		time.AfterFunc(interruptDelay, func() { cancel(shutdown.InterruptCause(syscall.SIGINT)) })
	}
	run := runWithRecorder(t, ctx, rec, slowFailingScript, map[string]FlagValue{"--video": true})

	if run.code == 0 {
		t.Fatalf("exit code = 0 for an interrupted run")
	}
	if rec.stops != 0 || rec.discards != 1 {
		t.Errorf("stops=%d discards=%d, want 0, 1", rec.stops, rec.discards)
	}
	if run.meta(t).Recording != nil {
		t.Errorf("meta recording = %+v, want none", run.meta(t).Recording)
	}
	for _, name := range artifactNames(t, run.artifacts) {
		if strings.Contains(name, "video") {
			t.Errorf("interrupted run kept %s", name)
		}
	}
}

func TestRunVideoOffMakesNoRecorderCall(t *testing.T) {
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	run := runWithRecorder(t, context.Background(), rec, failingScript, nil)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1", run.code)
	}
	if rec.starts+rec.stops+rec.discards != 0 {
		t.Errorf("recorder called: starts=%d stops=%d discards=%d", rec.starts, rec.stops, rec.discards)
	}
	if run.meta(t).Recording != nil || strings.Contains(run.stdout, `"recording"`) {
		t.Errorf("a run with video off reports a recording")
	}
}
