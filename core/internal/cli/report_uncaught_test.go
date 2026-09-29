package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

// TestReportUncaught_CodedErrorPrintsCodeMessageAndRemedy guards the
// CLI's single error-reporting path: a coded error's code, message,
// and remedy must each reach stderr, the remedy on its own line.
func TestReportUncaught_CodedErrorPrintsCodeMessageAndRemedy(t *testing.T) {
	err := diag.New(diag.CodeDeviceNotFound, "no active device", "select a device first, then retry", nil)

	var stderr bytes.Buffer
	exitCode := reportUncaught(&stderr, false, err)

	if exitCode != 1 {
		t.Fatalf("reportUncaught() exit code = %d, want 1", exitCode)
	}
	lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("reportUncaught() stderr lines = %v, want exactly 2", lines)
	}
	if want := "error [DEVICE_NOT_FOUND]: no active device"; lines[0] != want {
		t.Fatalf("reportUncaught() line 1 = %q, want %q", lines[0], want)
	}
	if want := "select a device first, then retry"; lines[1] != want {
		t.Fatalf("reportUncaught() line 2 = %q, want %q", lines[1], want)
	}
}

// TestReportUncaught_PlainErrorPrintsUnknownCodeAndNoRemedyLine covers
// a failure with no attached code: it still prints the "error [CODE]:
// message" line, using UNKNOWN, and prints no second line since there
// is no remedy to report.
func TestReportUncaught_PlainErrorPrintsUnknownCodeAndNoRemedyLine(t *testing.T) {
	err := errors.New("could not read the script file")

	var stderr bytes.Buffer
	reportUncaught(&stderr, false, err)

	want := "error [UNKNOWN]: could not read the script file\n"
	if got := stderr.String(); got != want {
		t.Fatalf("reportUncaught() stderr = %q, want %q", got, want)
	}
}
