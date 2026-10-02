package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

var failureScreenshot = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 7, 7}

type screenshotMockFactory struct{}

func (screenshotMockFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.ScreenshotData = failureScreenshot
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func runScriptText(t *testing.T, text string) (code int, artifactsDir string) {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	reportDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: screenshotMockFactory{},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code = RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath, "--report-dir": reportDir})
	return code, runfolder.ArtifactsDir(singleRunDir(t, reportDir))
}

func artifactNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestRunScriptFailedStepWritesFailureBundleIntoTheRunArtifacts(t *testing.T) {
	code, artifacts := runScriptText(t, "appId: com.not.installed\n---\n- sleep: 1\n- launchApp\n")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	got := strings.Join(artifactNames(t, artifacts), ",")
	want := "step-002-failure.png,step-002-log.jsonl,step-002-tree.json"
	if got != want {
		t.Fatalf("artifacts = %s, want %s", got, want)
	}
	data, err := os.ReadFile(filepath.Join(artifacts, "step-002-failure.png"))
	if err != nil || !bytes.Equal(data, failureScreenshot) {
		t.Fatalf("failure screenshot = %v, %v", data, err)
	}
}

func TestRunScriptPassingRunWritesNoFailureBundle(t *testing.T) {
	code, artifacts := runScriptText(t, "- sleep: 1\n")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, name := range artifactNames(t, artifacts) {
		if strings.HasPrefix(name, "step-") {
			t.Fatalf("passing run wrote %s", name)
		}
	}
}

func runWithFactory(t *testing.T, flags map[string]FlagValue, suitePositional *string, isSuite bool) string {
	t.Helper()
	reportDir := t.TempDir()
	flags["--report-dir"] = reportDir
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: screenshotMockFactory{},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	if isSuite {
		RunSuite(context.Background(), deps, flags, suitePositional)
	} else {
		RunScript(context.Background(), deps, flags)
	}
	return runfolder.ArtifactsDir(singleRunDir(t, reportDir))
}

func TestRunScriptScenarioWritesScreenshotsAndFailureEvidencePerChild(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c0.yml", "- screenshot: a\n")
	writeFile(t, dir, "c1.yml", "- screenshot: a\n- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "c2.yml", "- screenshot: a\n")
	writeFile(t, dir, "scenario.yml", "name: S\nscripts:\n  - c0.yml\n  - c1.yml\n  - c2.yml\n")

	artifacts := runWithFactory(t, map[string]FlagValue{"--file": filepath.Join(dir, "scenario.yml")}, nil, false)

	got := strings.Join(artifactNames(t, artifacts), ",")
	want := strings.Join([]string{
		"01-0-c0-yml-a.png",
		"02-1-c1-yml-a.png",
		"1-c1-yml-step-002-failure.png",
		"1-c1-yml-step-002-log.jsonl",
		"1-c1-yml-step-002-tree.json",
	}, ",")
	if got != want {
		t.Fatalf("artifacts = %s, want %s", got, want)
	}
}

func TestRunSuiteWritesScreenshotsAndFailureEvidencePerEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "a.yml", "- screenshot: a\n")
	writeFile(t, dir, "b.yml", "- assertExpr: { expr: '1 == 2' }\n")

	artifacts := runWithFactory(t, map[string]FlagValue{"--glob": []string{"*.yml"}}, nil, true)

	var screenshots, evidence []string
	for _, name := range artifactNames(t, artifacts) {
		if strings.HasPrefix(name, "01-0-") {
			screenshots = append(screenshots, name)
		}
		if strings.HasPrefix(name, "1-") && strings.Contains(name, "-step-001-") {
			evidence = append(evidence, name)
		}
	}
	if len(screenshots) != 1 {
		t.Fatalf("screenshots = %v in %v", screenshots, artifactNames(t, artifacts))
	}
	if len(evidence) != 3 {
		t.Fatalf("evidence = %v in %v", evidence, artifactNames(t, artifacts))
	}
}
