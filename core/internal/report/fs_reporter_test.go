package report

import (
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/runner"
)

func TestFsReporter_WritesExpectedPaths(t *testing.T) {
	outDir := t.TempDir()

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

	bundle, err := NewFsReporter().Generate(input, ReportOptions{Formats: []string{FormatHTML, FormatJSON}, OutDir: outDir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	bundleDir := filepath.Join(outDir, "run-123")
	if bundle.BundleDir != bundleDir {
		t.Errorf("expected bundle dir %q, got %q", bundleDir, bundle.BundleDir)
	}

	wantFiles := []string{
		filepath.Join(bundleDir, "screenshots", "01-after-login.png"),
		filepath.Join(bundleDir, "report.html"),
		filepath.Join(bundleDir, "report.json"),
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
