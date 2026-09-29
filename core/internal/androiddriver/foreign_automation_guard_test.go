package androiddriver

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
)

// canned dumpsys fixtures. positiveDumpsysOutput is trimmed from a
// real "dumpsys activity" dump's instrumentation section; the parts
// this package does not look at are omitted, but the literal
// instrumentation-record marker is kept verbatim.
const positiveDumpsysOutput = `ACTIVITY MANAGER INSTRUMENTATION (dumpsys activity instrumentation)
  * InstrumentationRecord{9f3a2b0 com.example.app.test/androidx.test.runner.AndroidJUnitRunner}
      base=com.example.app.test resultClass=null
`

const cleanDumpsysOutput = `ACTIVITY MANAGER INSTRUMENTATION (dumpsys activity instrumentation)
ACTIVITY MANAGER RUNNING PROCESSES (dumpsys activity processes)
  * ProcessRecord{a1b2c3d 12345:com.example.app/u0a123}
`

func TestHasActiveForeignInstrumentation_PositiveOutput_ReportsTrue(t *testing.T) {
	if !hasActiveForeignInstrumentation(positiveDumpsysOutput) {
		t.Fatal("hasActiveForeignInstrumentation() = false, want true for an output carrying an instrumentation record")
	}
}

func TestHasActiveForeignInstrumentation_CleanOutput_ReportsFalse(t *testing.T) {
	if hasActiveForeignInstrumentation(cleanDumpsysOutput) {
		t.Fatal("hasActiveForeignInstrumentation() = true, want false for an output with no instrumentation record")
	}
}

func TestCheckForForeignAutomation_ActiveInstrumentation_ReturnsDeviceBusy(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "dumpsys", "activity"),
		process.Result{Stdout: positiveDumpsysOutput}, nil)

	err := checkForForeignAutomation(context.Background(), runner, "S1")
	if err == nil {
		t.Fatal("checkForForeignAutomation() error = nil, want a DEVICE_BUSY diagnostic")
	}
	code, _, remedy := diag.Describe(err)
	if code != diag.CodeDeviceBusy {
		t.Fatalf("checkForForeignAutomation() code = %q, want %q", code, diag.CodeDeviceBusy)
	}
	if remedy == "" {
		t.Fatal("checkForForeignAutomation() remedy is empty, want an actionable hint")
	}
}

func TestCheckForForeignAutomation_NoInstrumentation_ReturnsNil(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "dumpsys", "activity"),
		process.Result{Stdout: cleanDumpsysOutput}, nil)

	if err := checkForForeignAutomation(context.Background(), runner, "S1"); err != nil {
		t.Fatalf("checkForForeignAutomation() error = %v, want nil for a clean dump", err)
	}
}

// TestCheckForForeignAutomation_ProbeError_FailsOpen proves the guard
// never blocks a run on a probe failure: an errored dumpsys call
// carries no information about whether a foreign session is running,
// so it must be treated the same as a clean result.
func TestCheckForForeignAutomation_ProbeError_FailsOpen(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "dumpsys", "activity"),
		process.Result{}, errors.New("device offline"))

	if err := checkForForeignAutomation(context.Background(), runner, "S1"); err != nil {
		t.Fatalf("checkForForeignAutomation() error = %v, want nil when the probe itself fails", err)
	}
}
