package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/runfolder"
)

type descriptorRecordingFactory struct {
	screenshotMockFactory
	built []device.Descriptor
}

func (f *descriptorRecordingFactory) Build(ctx context.Context, d device.Descriptor) (device.Built, error) {
	f.built = append(f.built, d)
	return f.screenshotMockFactory.Build(ctx, d)
}

func (f *descriptorRecordingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory { return f }

func TestParseArgvSuiteAcceptsDeviceAndJSON(t *testing.T) {
	result := mustParseArgv(t, []string{"suite", "smoke", "--device", "emulator-5556", "--json", "--report", "junit"})
	if result.Flags["--device"] != "emulator-5556" || !isTruthy(result.Flags["--json"]) || result.Flags["--report"] != "junit" {
		t.Errorf("flags = %v", result.Flags)
	}
}

func TestRunSuiteDeviceFlagPicksTheDeviceTheSuiteRunsOn(t *testing.T) {
	cases := []struct {
		name   string
		flags  map[string]FlagValue
		wantID string
	}{
		{"flag", map[string]FlagValue{"--device": "emulator-5556"}, "emulator-5556"},
		{"default", map[string]FlagValue{}, "emulator-5554"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeSuiteFlows(t)
			factory := &descriptorRecordingFactory{}
			tc.flags["--glob"] = []string{"b_*.yml"}
			run := runRetrySuite(t, factory, nil, tc.flags, nil)

			if run.code != 0 {
				t.Fatalf("exit code = %d; stderr: %s", run.code, run.stderr)
			}
			if len(factory.built) != 1 || factory.built[0].ID != tc.wantID {
				t.Errorf("built %+v, want one device %s", factory.built, tc.wantID)
			}
		})
	}
}

func TestRunSuiteJSONPrintsOneObjectOnStdoutAndTheSummaryOnStderr(t *testing.T) {
	writeSuiteFlows(t)
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &stdout, Stderr: &stderr}

	code := RunSuite(context.Background(), deps, map[string]FlagValue{"--glob": []string{"*.yml"}, "--report-dir": root, "--json": true}, nil)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	runDir := singleRunDir(t, root)
	decoder := json.NewDecoder(&stdout)
	var payload suiteJSONResult
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("stdout is not a JSON object: %v", err)
	}
	if decoder.More() {
		t.Error("stdout carries more than one JSON value")
	}
	if payload.Ok || payload.RunFolder != runDir {
		t.Errorf("payload = ok %v, folder %q, want a failed suite in %q", payload.Ok, payload.RunFolder, runDir)
	}
	if !reflect.DeepEqual(payload.Entries, readIndex(t, runDir).Entries) {
		t.Errorf("payload entries differ from index.json:\n%+v\n%+v", payload.Entries, readIndex(t, runDir).Entries)
	}
	if !strings.Contains(stderr.String(), "FAIL: suite") || strings.Contains(stdout.String(), "flows passed") {
		t.Errorf("the summary is not on stderr only; stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "Run folder:") {
		t.Errorf("the run folder is printed twice: %s", stderr.String())
	}
}

func TestRunSuiteWithoutJSONKeepsStdoutHumanReadable(t *testing.T) {
	writeSuiteFlows(t)
	var stdout bytes.Buffer
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &stdout, Stderr: &bytes.Buffer{}}

	RunSuite(context.Background(), deps, map[string]FlagValue{"--glob": []string{"*.yml"}, "--report-dir": t.TempDir()}, nil)

	if !strings.Contains(stdout.String(), "flows passed") || strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("stdout = %q, want the text summary", stdout.String())
	}
}

type junitTestCase struct {
	Name    string    `xml:"name,attr"`
	Failure *struct{} `xml:"failure"`
	Error   *struct{} `xml:"error"`
	Skipped *struct {
		Message string `xml:"message,attr"`
	} `xml:"skipped"`
	Flaky []struct{} `xml:"flakyFailure"`
}

type junitTestSuite struct {
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

func TestRunSuiteJUnitAndIndexDescribeTheSameEntries(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "f_flaky.yml", readyScript)
	writeFile(t, dir, "q_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "z_fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "atomyx.config.yaml", suiteConfig("    retries: 1\n    quarantine: [\"q_*.yml\"]\n"))
	name := "nightly"
	factory := &flakyFactory{}
	rec := &fakeVideoRecorder{segments: []string{".tmp-a.mp4"}}
	rec.onStart = func() {
		if rec.starts == 2 {
			factory.show()
		}
	}

	run := runRetrySuite(t, factory, rec, map[string]FlagValue{"--config": filepath.Join(dir, "atomyx.config.yaml"), "--video": true, "--report": "junit"}, &name)

	if run.code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", run.code, run.stderr)
	}
	body, err := os.ReadFile(filepath.Join(runfolder.ReportDir(run.runDir), runfolder.JUnitFileName))
	if err != nil {
		t.Fatalf("no junit report: %v", err)
	}
	var suite junitTestSuite
	if err := xml.Unmarshal(body, &suite); err != nil {
		t.Fatal(err)
	}
	index := readIndex(t, run.runDir)
	if suite.Tests != len(index.Entries) || len(suite.Cases) != len(index.Entries) {
		t.Fatalf("junit has %d cases, index has %d entries", len(suite.Cases), len(index.Entries))
	}
	var failed, quarantined int
	for i, entry := range index.Entries {
		testCase := suite.Cases[i]
		failedAttempts := 0
		for _, attempt := range entry.Attempts {
			if attempt.Result == runfolder.ResultFailed {
				failedAttempts++
			}
		}
		switch {
		case entry.Status == runfolder.ResultPassed:
			if testCase.Failure != nil || testCase.Skipped != nil || len(testCase.Flaky) != failedAttempts || entry.Flaky != (failedAttempts > 0) {
				t.Errorf("entry %s passed in the index, junit case = %+v", entry.Path, testCase)
			}
		case entry.Quarantined:
			quarantined++
			if testCase.Skipped == nil || testCase.Skipped.Message != "quarantined" {
				t.Errorf("entry %s is a quarantined failure, junit case = %+v", entry.Path, testCase)
			}
		default:
			failed++
			if testCase.Failure == nil {
				t.Errorf("entry %s failed in the index, junit case = %+v", entry.Path, testCase)
			}
		}
		if testCase.Name != entry.Path {
			t.Errorf("case %d is %q, index entry is %q", i, testCase.Name, entry.Path)
		}
	}
	if suite.Failures+suite.Errors != failed || suite.Skipped != quarantined || failed != 1 || quarantined != 1 {
		t.Errorf("junit counts %+v, index has %d failed and %d quarantined", suite, failed, quarantined)
	}
}

func TestRunScriptJUnitReportHoldsOneTestCase(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	writeFile(t, dir, "flow.yml", "- assertExpr: { expr: '1 == 2' }\n")
	root := t.TempDir()
	deps := Deps{Probe: device.NewStaticProbe(nil), Factory: screenshotMockFactory{}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath, "--report": "junit", "--report-dir": root})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	body, err := os.ReadFile(filepath.Join(runfolder.ReportDir(singleRunDir(t, root)), runfolder.JUnitFileName))
	if err != nil {
		t.Fatal(err)
	}
	var suite junitTestSuite
	if err := xml.Unmarshal(body, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Tests != 1 || suite.Failures != 1 || len(suite.Cases) != 1 || suite.Cases[0].Failure == nil {
		t.Errorf("junit suite = %+v, want one failed test case", suite)
	}
}
