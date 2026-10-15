package mcp

import (
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
)

func TestFailureCodeOf(t *testing.T) {
	failed := 0
	stepCode := runner.ScriptResult{FailedAtStep: &failed, Steps: []runner.StepResult{{StepIndex: 0, Code: diag.CodeToolTimeout}}}
	coded := diag.New(diag.CodeAgentNotReady, "not ready", "", nil)
	cases := []struct {
		name    string
		outcome runfolder.Outcome
		result  runner.ScriptResult
		err     error
		want    string
	}{
		{"passed", runfolder.OutcomePassed, stepCode, nil, ""},
		{"cancelled", runfolder.OutcomeCancelled, stepCode, nil, ""},
		{"failed step code", runfolder.OutcomeFailed, stepCode, nil, diag.CodeToolTimeout},
		{"failed no code", runfolder.OutcomeFailed, runner.ScriptResult{}, nil, ""},
		{"error coded", runfolder.OutcomeError, runner.ScriptResult{}, coded, diag.CodeAgentNotReady},
		{"error uncoded", runfolder.OutcomeError, runner.ScriptResult{}, errors.New("boom"), ""},
	}
	for _, c := range cases {
		if got := failureCodeOf(c.outcome, c.result, c.err); got != c.want {
			t.Errorf("%s: failureCodeOf = %q, want %q", c.name, got, c.want)
		}
	}
}
