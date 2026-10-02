package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
)

func TestFsReporter_WritesExpectedPaths(t *testing.T) {
	runDir := t.TempDir()
	reportDir := filepath.Join(runDir, "report")
	artifactsDir := filepath.Join(runDir, "artifacts")

	input := ScriptReportInput{
		Metadata: ReportMetadata{RunID: "run-123", Label: "Login flow", Command: ReportCommandRun},
		Result: runner.ScriptResult{
			ScriptName:  "Login flow",
			Ok:          true,
			Steps:       []runner.StepResult{{StepIndex: 0, Command: "launchApp", Ok: true, DurationMs: 100}},
			TotalSteps:  1,
			PassedSteps: 1,
			DurationMs:  100,
		},
		Screenshots: []ReportScreenshot{
			{Scope: RootScriptScope, Label: "after login", Data: []byte("fake-png-bytes")},
		},
	}

	bundle, err := NewFsReporter().Generate(input, ReportOptions{Formats: []string{FormatHTML, FormatJSON}, ReportDir: reportDir, ArtifactsDir: artifactsDir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if bundle.BundleDir != reportDir {
		t.Errorf("expected report dir %q, got %q", reportDir, bundle.BundleDir)
	}

	wantFiles := []string{
		filepath.Join(artifactsDir, "01-after-login.png"),
		filepath.Join(reportDir, "report.html"),
		filepath.Join(reportDir, "report.json"),
	}
	if len(bundle.Files) != len(wantFiles) {
		t.Fatalf("expected %d files, got %d: %v", len(wantFiles), len(bundle.Files), bundle.Files)
	}
	for i, want := range wantFiles {
		if bundle.Files[i] != want {
			t.Errorf("file %d: expected %q, got %q", i, want, bundle.Files[i])
		}
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %q to exist on disk: %v", want, err)
		}
	}

	screenshotBytes, err := os.ReadFile(wantFiles[0])
	if err != nil {
		t.Fatalf("reading written screenshot: %v", err)
	}
	if string(screenshotBytes) != "fake-png-bytes" {
		t.Errorf("expected screenshot bytes to round-trip, got %q", screenshotBytes)
	}
}

func TestFsReporter_HTMLImageLinksLeadFromReportDirToArtifactsDir(t *testing.T) {
	runDir := t.TempDir()
	reportDir := filepath.Join(runDir, "report")
	artifactsDir := filepath.Join(runDir, "artifacts")
	input := ScriptReportInput{
		Metadata:    ReportMetadata{RunID: "run-1", Command: ReportCommandRun},
		Result:      runner.ScriptResult{ScriptName: "Flow", Ok: true},
		Screenshots: []ReportScreenshot{{Scope: RootScriptScope, Label: "home", Data: []byte("png")}},
	}

	if _, err := NewFsReporter().Generate(input, ReportOptions{Formats: []string{FormatHTML}, ReportDir: reportDir, ArtifactsDir: artifactsDir}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	html, err := os.ReadFile(filepath.Join(reportDir, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `<img src="../artifacts/01-home.png"`) {
		t.Errorf("report.html does not link the image as ../artifacts/01-home.png:\n%s", html)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, "01-home.png")); err != nil {
		t.Errorf("screenshot missing from the artifacts directory: %v", err)
	}
}

func TestFsReporter_NoFormatsWritesScreenshotsOnly(t *testing.T) {
	runDir := t.TempDir()
	reportDir := filepath.Join(runDir, "report")
	artifactsDir := filepath.Join(runDir, "artifacts")
	input := ScriptReportInput{
		Metadata:    ReportMetadata{RunID: "run-1", Command: ReportCommandRun},
		Result:      runner.ScriptResult{ScriptName: "Flow", Ok: true},
		Screenshots: []ReportScreenshot{{Scope: RootScriptScope, Label: "home", Data: []byte("png")}},
	}

	if _, err := NewFsReporter().Generate(input, ReportOptions{ReportDir: reportDir, ArtifactsDir: artifactsDir}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artifactsDir, "01-home.png")); err != nil {
		t.Errorf("screenshot missing from the artifacts directory: %v", err)
	}
	if _, err := os.Stat(reportDir); !os.IsNotExist(err) {
		t.Errorf("report directory exists although no format was requested: %v", err)
	}
}

func TestFsReporter_RejectsMissingDirectories(t *testing.T) {
	input := ScriptReportInput{Metadata: ReportMetadata{RunID: "run-1"}, Result: runner.ScriptResult{Ok: true}}
	if _, err := NewFsReporter().Generate(input, ReportOptions{}); err == nil {
		t.Error("Generate without an artifacts directory succeeded, want an error")
	}
	if _, err := NewFsReporter().Generate(input, ReportOptions{Formats: []string{FormatHTML}, ArtifactsDir: t.TempDir()}); err == nil {
		t.Error("Generate with a format and no report directory succeeded, want an error")
	}
}
