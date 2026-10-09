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

func suiteDeps(rec device.ScreenRecorder, stderr *bytes.Buffer) Deps {
	return Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &bytes.Buffer{}, Stderr: stderr, Recorder: rec}
}

func writeSuiteFlows(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "b_pass.yml", "- sleep: 1\n")
	writeFile(t, dir, "c_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
}

func TestRunSuiteVideoKeepsOneRecordingPerFailedEntryWithoutCollisions(t *testing.T) {
	writeSuiteFlows(t)
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	reportDir := t.TempDir()
	var stderr bytes.Buffer

	code := RunSuite(context.Background(), suiteDeps(rec, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report": "html,json", "--report-dir": reportDir, "--video": true,
	}, nil)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if rec.starts != 3 || rec.stops != 2 || rec.discards != 1 {
		t.Errorf("starts=%d stops=%d discards=%d, want 3, 2, 1", rec.starts, rec.stops, rec.discards)
	}
	runDir := singleRunDir(t, reportDir)
	names := strings.Join(artifactNames(t, runfolder.ArtifactsDir(runDir)), ",")
	for _, want := range []string{"video-entry-1-1.mp4", "video-entry-3-1.mp4"} {
		if !strings.Contains(names, want) {
			t.Errorf("artifacts %s miss %s", names, want)
		}
	}
	if strings.Contains(names, "video-entry-2") {
		t.Errorf("the passed entry kept a video: %s", names)
	}
	meta := videoRun{runDir: runDir}.meta(t)
	if meta.Recording != nil {
		t.Errorf("a suite meta carries a recording: %+v", meta.Recording)
	}

	raw := findReportJSON(t, reportDir)
	var decoded struct {
		Result struct {
			Entries []struct {
				Videos []string `json:"videos"`
			} `json:"entries"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	got := [][]string{}
	for _, e := range decoded.Result.Entries {
		got = append(got, e.Videos)
	}
	if len(got) != 3 || strings.Join(got[0], ",") != "video-entry-1-1.mp4" || len(got[1]) != 0 || strings.Join(got[2], ",") != "video-entry-3-1.mp4" {
		t.Errorf("report entry videos = %v", got)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(runDir, "report", "report.html"))
	if err != nil || !strings.Contains(string(htmlBytes), `href="../artifacts/video-entry-3-1.mp4"`) {
		t.Errorf("html does not link the third entry's video (%v)", err)
	}
	if strings.Count(stderr.String(), "Video: ") != 2 {
		t.Errorf("stderr lists the videos wrongly: %s", stderr.String())
	}
}

func TestRunSuiteVideoOffMakesNoRecorderCall(t *testing.T) {
	writeSuiteFlows(t)
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	var stderr bytes.Buffer

	code := RunSuite(context.Background(), suiteDeps(rec, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report-dir": t.TempDir(),
	}, nil)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if rec.starts+rec.stops+rec.discards != 0 {
		t.Errorf("recorder called: starts=%d stops=%d discards=%d", rec.starts, rec.stops, rec.discards)
	}
}

func TestRunSuiteVideoInterruptedEntryDiscards(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a_fail.yml", "appId: com.not.installed\n---\n- sleep: 800\n- launchApp\n")
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	ctx, cancel := context.WithCancelCause(context.Background())
	rec.onStart = func() {
		time.AfterFunc(interruptDelay, func() { cancel(shutdown.InterruptCause(syscall.SIGINT)) })
	}
	reportDir := t.TempDir()
	var stderr bytes.Buffer

	RunSuite(ctx, suiteDeps(rec, &stderr), map[string]FlagValue{
		"--glob": []string{"*.yml"}, "--report-dir": reportDir, "--video": true,
	}, nil)

	if rec.stops != 0 || rec.discards != 1 {
		t.Errorf("stops=%d discards=%d, want 0, 1", rec.stops, rec.discards)
	}
	for _, name := range artifactNames(t, runfolder.ArtifactsDir(singleRunDir(t, reportDir))) {
		if strings.Contains(name, "video") {
			t.Errorf("an interrupted entry kept %s", name)
		}
	}
}
