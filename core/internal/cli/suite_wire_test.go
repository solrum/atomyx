package cli

import (
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

func TestWireEntryCarriesTheFailureTextOfAFailedTestOnly(t *testing.T) {
	detail := "gone"
	failedRun := &scenario.SuiteEntryOutcome{Script: &runner.ScriptResult{Steps: []runner.StepResult{{StepIndex: 1, Command: "tap", Detail: &detail}}}}
	loadError := "parse error"
	entry := scenario.SuiteEntryResult{Path: "a.yml", Kind: scenario.KindScript, Status: scenario.EntryFailed, Attempts: []scenario.SuiteAttempt{
		{Number: 1, Status: scenario.EntryFailed, Result: failedRun},
		{Number: 2, Status: scenario.EntryFailed, Error: &loadError},
		{Number: 3, Status: scenario.EntryPassed, Result: failedRun},
	}}

	wire := wireEntry(entry)

	if wire.Attempts[0].Failure != "step 2 (tap) failed: gone" {
		t.Errorf("failed test failure = %q", wire.Attempts[0].Failure)
	}
	if wire.Attempts[1].Failure != "" || wire.Attempts[1].Error == nil || wire.Attempts[2].Failure != "" {
		t.Errorf("attempts = %+v, want no failure text for an unloadable or passed attempt", wire.Attempts[1:])
	}
	if back := suiteEntryFromWire(wire); back.Attempts[0].Failure != wire.Attempts[0].Failure {
		t.Errorf("the failure text did not survive the trip: %+v", back.Attempts[0])
	}
}
