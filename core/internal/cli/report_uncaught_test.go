package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/shutdown"
)

// TestReportUncaught_CodedErrorPrintsCodeMessageAndRemedy guards the
// CLI's single error-reporting path: a coded error's code, message,
// and remedy must each reach stderr, the remedy on its own line.
func TestReportUncaught_CodedErrorPrintsCodeMessageAndRemedy(t *testing.T) {
	err := diag.New(diag.CodeDeviceNotFound, "no active device", "select a device first, then retry", nil)

	var stderr bytes.Buffer
	exitCode := reportUncaught(context.Background(), &stderr, false, err)

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
	reportUncaught(context.Background(), &stderr, false, err)

	want := "error [UNKNOWN]: could not read the script file\n"
	if got := stderr.String(); got != want {
		t.Fatalf("reportUncaught() stderr = %q, want %q", got, want)
	}
}

// TestReportUncaught_SignalCancelPrintsInterrupted guards the user
// interrupt path: a context a stop signal canceled yields one
// "interrupted" line and the signal's conventional exit code instead of
// an UNKNOWN error.
func TestReportUncaught_SignalCancelPrintsInterrupted(t *testing.T) {
	cases := []struct {
		sig  os.Signal
		want int
	}{
		{syscall.SIGINT, shutdown.ExitCodeSigint},
		{syscall.SIGTERM, shutdown.ExitCodeSigterm},
	}
	for _, c := range cases {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(shutdown.InterruptCause(c.sig))

		var stderr bytes.Buffer
		got := reportUncaught(ctx, &stderr, false, context.Canceled)
		if got != c.want {
			t.Errorf("%v: exit code = %d, want %d", c.sig, got, c.want)
		}
		if out := stderr.String(); out != "interrupted\n" {
			t.Errorf("%v: stderr = %q, want %q", c.sig, out, "interrupted\n")
		}
	}
}

// TestReportUncaught_UnrelatedCancelStaysAnError guards the detection
// boundary: a context.Canceled that did not come from a stop signal is
// still reported as an ordinary error with exit code 1.
func TestReportUncaught_UnrelatedCancelStaysAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stderr bytes.Buffer
	if got := reportUncaught(ctx, &stderr, false, context.Canceled); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
	if strings.Contains(stderr.String(), "interrupted") {
		t.Errorf("stderr = %q, must not report an interrupt", stderr.String())
	}
}

// TestRunContext_InterruptKeepsPartialFailureExitCode guards the
// graceful path: a command that returns a failure code after a stop
// signal canceled the context reports the signal's exit code, while a
// command that succeeded keeps 0.
func TestRunContext_InterruptKeepsPartialFailureExitCode(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(shutdown.InterruptCause(syscall.SIGINT))

	var stderr bytes.Buffer
	got := RunContext(ctx, []string{"nonexistent-command"}, Deps{Stderr: &stderr, Stdout: &bytes.Buffer{}})
	if got != shutdown.ExitCodeSigint {
		t.Errorf("exit code = %d, want %d", got, shutdown.ExitCodeSigint)
	}
	if got := RunContext(ctx, []string{"version"}, Deps{Stdout: &bytes.Buffer{}, Stderr: &stderr}); got != 0 {
		t.Errorf("version exit code = %d, want 0", got)
	}
}
