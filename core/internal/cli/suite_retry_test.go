package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

// flakyFactory builds a mock device whose screen shows nothing until the
// test calls show, so a script that asserts the "ready" text fails until
// then.
type flakyFactory struct {
	screenshotMockFactory
	drv *mock.Driver
}

func (f *flakyFactory) Build(ctx context.Context, d device.Descriptor) (device.Built, error) {
	built, err := f.screenshotMockFactory.Build(ctx, d)
	if err != nil {
		return built, err
	}
	f.drv = built.Driver.(*mock.Driver)
	return built, nil
}

func (f *flakyFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory { return f }

func (f *flakyFactory) show() {
	f.drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{}, Children: []wire.TreeNodeWire{
		{Attributes: map[string]string{"text": "ready"}, Children: []wire.TreeNodeWire{}},
	}})
}

const readyScript = "- assertVisible:\n    text: ready\n    timeout: 50\n- screenshot: after\n"

type suiteRetryRun struct {
	code   int
	stderr string
	root   string
	runDir string
}

func runRetrySuite(t *testing.T, factory device.WrappingFactory, rec device.ScreenRecorder, flags map[string]FlagValue, positional *string) suiteRetryRun {
	t.Helper()
	root := t.TempDir()
	var stderr bytes.Buffer
	flags["--report-dir"] = root
	if _, ok := flags["--report"]; !ok {
		flags["--report"] = "html,json"
	}
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: factory, Stdout: &bytes.Buffer{}, Stderr: &stderr, Recorder: rec}
	code := RunSuite(context.Background(), deps, flags, positional)
	return suiteRetryRun{code: code, stderr: stderr.String(), root: root, runDir: singleRunDir(t, root)}
}

func (r suiteRetryRun) names(t *testing.T) []string {
	t.Helper()
	return artifactNames(t, runfolder.ArtifactsDir(r.runDir))
}

func (r suiteRetryRun) stepAttempts(t *testing.T) map[int]int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(r.runDir, runfolder.StepsFileName))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var decoded struct {
			Attempt int `json:"attempt"`
		}
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatal(err)
		}
		counts[decoded.Attempt]++
	}
	return counts
}

func writeReadyFlow(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "flow.yml", readyScript)
}

func TestRunSuiteRetriesAFlakyEntryAndExitsZero(t *testing.T) {
	writeReadyFlow(t)
	factory := &flakyFactory{}
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	rec.onStart = func() {
		if rec.starts == 2 {
			factory.show()
		}
	}

	run := runRetrySuite(t, factory, rec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "2", "--video": true}, nil)

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", run.code, run.stderr)
	}
	if meta := (videoRun{runDir: run.runDir}).meta(t); meta.Result != runfolder.ResultPassed {
		t.Errorf("meta result = %q, want passed", meta.Result)
	}
	if strings.Join(rec.names, ",") != "video-entry-1,video-entry-1-attempt-2" {
		t.Errorf("recordings = %v, want one per attempt", rec.names)
	}
	var decoded struct {
		Result struct {
			Flaky   int `json:"flaky"`
			Entries []struct {
				Flaky    bool     `json:"flaky"`
				Videos   []string `json:"videos"`
				Attempts []struct {
					Number int      `json:"number"`
					Status string   `json:"status"`
					Videos []string `json:"videos"`
				} `json:"attempts"`
			} `json:"entries"`
		} `json:"result"`
	}
	if err := json.Unmarshal(findReportJSON(t, run.root), &decoded); err != nil {
		t.Fatal(err)
	}
	entry := decoded.Result.Entries[0]
	if decoded.Result.Flaky != 1 || !entry.Flaky || len(entry.Attempts) != 2 {
		t.Fatalf("report = %+v, want one flaky entry with two attempts", decoded.Result)
	}
	if entry.Attempts[0].Status != "failed" || entry.Attempts[1].Status != "passed" {
		t.Errorf("attempt statuses = %+v", entry.Attempts)
	}
	if len(entry.Attempts[0].Videos) != 1 || len(entry.Attempts[1].Videos) != 0 {
		t.Errorf("attempt videos = %+v, want the failed attempt's video only", entry.Attempts)
	}
	html := string(findReportHTML(t, run.root))
	if !strings.Contains(html, "flaky (2 attempts)") {
		t.Errorf("the html report has no flaky badge")
	}
	attempts := run.stepAttempts(t)
	if attempts[0] == 0 || attempts[2] == 0 || len(attempts) != 2 {
		t.Errorf("step lines by attempt = %v, want lines for attempt 1 (no field) and attempt 2", attempts)
	}
}

func TestRunSuiteExhaustedRetriesFailTheSuite(t *testing.T) {
	writeReadyFlow(t)
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}

	run := runRetrySuite(t, &flakyFactory{}, rec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "1", "--video": true}, nil)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1", run.code)
	}
	if rec.starts != 2 {
		t.Errorf("recordings = %d, want one per attempt", rec.starts)
	}
}

func TestRunSuiteFirstAttemptKeepsTheNamesOfASuiteWithoutRetries(t *testing.T) {
	writeReadyFlow(t)
	plainRec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	plain := runRetrySuite(t, &flakyFactory{}, plainRec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--video": true}, nil)
	retryRec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	retried := runRetrySuite(t, &flakyFactory{}, retryRec, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "1", "--video": true}, nil)

	plainNames := plain.names(t)
	retriedNames := strings.Join(retried.names(t), ",")
	if len(plainNames) == 0 {
		t.Fatal("the plain run wrote no artifacts")
	}
	for _, name := range plainNames {
		if !strings.Contains(","+retriedNames+",", ","+name+",") {
			t.Errorf("attempt 1 name %s is missing from the retried run: %s", name, retriedNames)
		}
	}
	extra := 0
	for _, name := range retried.names(t) {
		if !strings.Contains(","+strings.Join(plainNames, ",")+",", ","+name+",") {
			extra++
			if !strings.Contains(name, "attempt-2") {
				t.Errorf("later attempt file %s does not carry its attempt", name)
			}
		}
	}
	if extra == 0 {
		t.Error("attempt 2 wrote no files of its own")
	}
	if !strings.Contains(retriedNames, "video-entry-1-attempt-2-1.mp4") {
		t.Errorf("attempt 2 video missing: %s", retriedNames)
	}
}

func TestRunSuiteQuarantinedFailureKeepsExitZeroAndShowsInTheReport(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "b_pass.yml", "- sleep: 1\n")
	writeFile(t, dir, "atomyx.config.yaml", suiteConfig("    retries: 0\n    quarantine: [\"a_*.yml\"]\n"))
	name := "nightly"

	run := runRetrySuite(t, screenshotMockFactory{}, nil, map[string]FlagValue{"--config": filepath.Join(dir, "atomyx.config.yaml")}, &name)

	if run.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", run.code, run.stderr)
	}
	if meta := (videoRun{runDir: run.runDir}).meta(t); meta.Result != runfolder.ResultPassed {
		t.Errorf("meta result = %q, want passed", meta.Result)
	}
	html := string(findReportHTML(t, run.root))
	if !strings.Contains(html, "Quarantined failures") || !strings.Contains(html, "a_fail.yml") {
		t.Errorf("the html report does not list the quarantined failure")
	}
	var decoded struct {
		Result struct {
			QuarantinedFailures int `json:"quarantinedFailures"`
			Failed              int `json:"failed"`
		} `json:"result"`
	}
	if err := json.Unmarshal(findReportJSON(t, run.root), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Result.QuarantinedFailures != 1 || decoded.Result.Failed != 0 {
		t.Errorf("report counts = %+v, want one quarantined failure and no failure", decoded.Result)
	}
}

func suiteConfig(suiteBody string) string {
	return "project: Demo\napps:\n  - id: main\n    platform: android\n    appId: com.example.app\nenvironments:\n  staging:\n    app: main\nsuites:\n  nightly:\n    globs: [\"*.yml\"]\n" + suiteBody
}

func TestRunSuiteRetriesFlagOverridesTheConfigValue(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "atomyx.config.yaml", suiteConfig("    retries: 2\n"))
	name := "nightly"
	config := filepath.Join(dir, "atomyx.config.yaml")

	cases := []struct {
		name     string
		flags    map[string]FlagValue
		attempts int
	}{
		{"config value", map[string]FlagValue{"--config": config, "--video": true}, 3},
		{"flag lowers it", map[string]FlagValue{"--config": config, "--video": true, "--retries": "0"}, 1},
		{"flag raises it", map[string]FlagValue{"--config": config, "--video": true, "--retries": "3"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
			run := runRetrySuite(t, screenshotMockFactory{}, rec, tc.flags, &name)
			if run.code != 1 || rec.starts != tc.attempts {
				t.Errorf("exit %d with %d attempts, want exit 1 with %d", run.code, rec.starts, tc.attempts)
			}
		})
	}
}

func TestRunSuiteRejectsARetriesFlagOutOfRange(t *testing.T) {
	writeReadyFlow(t)
	root := t.TempDir()
	var stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: &flakyFactory{}, Stdout: &bytes.Buffer{}, Stderr: &stderr}

	code := RunSuite(context.Background(), deps, map[string]FlagValue{"--glob": []string{"*.yml"}, "--retries": "4", "--report-dir": root}, nil)

	if code != 2 || !strings.Contains(stderr.String(), "--retries") {
		t.Errorf("exit %d, stderr %q, want a usage error naming --retries", code, stderr.String())
	}
}
