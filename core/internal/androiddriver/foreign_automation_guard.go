package androiddriver

import (
	"context"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
)

// activeInstrumentationMarker is the token Android's activity manager
// prints for each live instrumentation record in a "dumpsys activity"
// dump. Its presence means some process on the device currently has
// an instrumentation attached, regardless of which app or test runner
// started it.
const activeInstrumentationMarker = "InstrumentationRecord{"

// foreignAutomationRemedy is the operator-facing action for a
// diag.CodeDeviceBusy failure.
const foreignAutomationRemedy = "Stop the other automation session driving this device, then try again."

// checkForForeignAutomation probes serial for a foreign automation
// session before Atomyx brings up its own agent or otherwise
// perturbs the device. The Atomyx Android agent drives entirely
// through an accessibility service and its own HTTP control server;
// it never starts an instrumentation. So an active instrumentation
// the device reports belongs to some other tool already driving it,
// and bringing up the Atomyx agent on top would race that tool for
// the same taps and gestures.
//
// This is a best-effort early warning, not a hard gate: the probe
// itself can fail in ordinary ways that say nothing about whether a
// foreign session is running, such as a slow device, a missing adb,
// or a command the process runner's own fallback timeout aborted. So
// every outcome other than a clear positive match returns nil and
// lets the caller proceed, because blocking a legitimate run on an
// inconclusive probe is worse than occasionally missing a foreign
// session.
func checkForForeignAutomation(ctx context.Context, proc process.Runner, serial string) error {
	result, err := proc.Run(ctx, "adb", "-s", serial, "shell", "dumpsys", "activity")
	if err != nil {
		return nil
	}
	if !hasActiveForeignInstrumentation(result.Stdout) {
		return nil
	}
	return diag.New(diag.CodeDeviceBusy,
		"another automation session appears to be driving this device already",
		foreignAutomationRemedy,
		nil)
}

// hasActiveForeignInstrumentation reports whether a "dumpsys activity"
// dump shows a currently active instrumentation. It looks for the
// literal instrumentation-record marker rather than parsing the
// dump's full section structure, because that marker is only printed
// when the activity manager has a live instrumentation record to
// report, which keeps the check conservative against unrelated
// changes elsewhere in the dump's format.
func hasActiveForeignInstrumentation(dumpsysOutput string) bool {
	return strings.Contains(dumpsysOutput, activeInstrumentationMarker)
}
