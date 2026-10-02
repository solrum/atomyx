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
