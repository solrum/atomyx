package scenario

import (
	"context"
	"testing"

	"dev.atomyx/internal/runner"
)

func failedScript(abort *runner.RunAbort) SuiteAttempt {
	return SuiteAttempt{Status: EntryFailed, Result: &SuiteEntryOutcome{Script: &runner.ScriptResult{Abort: abort}}}
}

func failedScenario(children ...ScenarioScriptResult) SuiteAttempt {
	return SuiteAttempt{Status: EntryFailed, Result: &SuiteEntryOutcome{Scenario: &ScenarioResult{Scripts: children}}}
}

func TestRetryableClassifiesEveryFailureKind(t *testing.T) {
	loadError := "bad yaml"
	canceled := context.Canceled
	cases := []struct {
		name    string
		ctxErr  error
		attempt SuiteAttempt
		want    bool
	}{
		{"a failed script step", nil, failedScript(nil), true},
		{"a failed scenario script", nil, failedScenario(ScenarioScriptResult{Status: StatusFailed, Result: &runner.ScriptResult{}}, ScenarioScriptResult{Status: StatusSkipped}), true},
		{"a canceled context", canceled, failedScript(nil), false},
		{"a lost device in a script", nil, failedScript(&runner.RunAbort{Reason: runner.RunAbortDeviceLost}), false},
		{"a lost device in a scenario script", nil, failedScenario(ScenarioScriptResult{Status: StatusFailed, Result: &runner.ScriptResult{Abort: &runner.RunAbort{Reason: runner.RunAbortDeviceLost}}}), false},
		{"a scenario script that could not load", nil, failedScenario(ScenarioScriptResult{Status: StatusErrored, Error: &loadError}), false},
		{"an entry that could not load", nil, SuiteAttempt{Status: EntryFailed, Error: &loadError}, false},
		{"a passed attempt", nil, SuiteAttempt{Status: EntryPassed, Result: &SuiteEntryOutcome{Script: &runner.ScriptResult{Ok: true}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(tc.ctxErr, tc.attempt); got != tc.want {
				t.Errorf("retryable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClampRetriesKeepsTheCountInRange(t *testing.T) {
	for in, want := range map[int]int{-2: 0, 0: 0, 2: 2, MaxRetries: MaxRetries, 9: MaxRetries} {
		if got := clampRetries(in); got != want {
			t.Errorf("clampRetries(%d) = %d, want %d", in, got, want)
		}
	}
}
