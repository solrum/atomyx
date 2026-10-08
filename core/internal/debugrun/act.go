package debugrun

import (
	"context"
	"strings"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
)

// defaultActBudget bounds one act so a hung device call cannot hold the
// session's debug lock forever.
const defaultActBudget = 30 * time.Second

// Act implements session.Debugger. The step runs in a child scope, so
// nothing it writes reaches the paused run's variables.
func (a *Adapter) Act(ctx context.Context, stepYAML string) (session.ActResult, error) {
	state := a.current()
	if state == nil {
		return session.ActResult{}, errNotPaused()
	}
	step, err := parseActStep(stepYAML)
	if err != nil {
		return session.ActResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, defaultActBudget)
	defer cancel()
	finish := a.collectArtifacts()
	started := time.Now()
	result, err := state.run.ExecuteStep(ctx, step)
	artifacts := finish()
	if err != nil {
		return session.ActResult{}, err
	}
	redactor := state.run.Variables().Redactor()
	out := session.ActResult{
		Command:    step.StepCommand(),
		Outcome:    "passed",
		DurationMs: time.Since(started).Milliseconds(),
		Artifacts:  artifacts,
		Step:       writtenText(step, redactor),
	}
	if !result.Ok {
		out.Outcome = "failed"
		if result.Detail != nil {
			out.Message = redactor.String(*result.Detail)
		}
	}
	return out, nil
}

// parseActStep reads exactly one step. The text may be a bare mapping
// such as "tap: Sign in" or a one-item sequence.
func parseActStep(stepYAML string) (script.ScriptStep, error) {
	text := strings.TrimSpace(stepYAML)
	if !strings.HasPrefix(text, "-") {
		text = "- " + strings.ReplaceAll(text, "\n", "\n  ")
	}
	def, err := script.ParseScript(text, nil)
	if err != nil {
		return nil, invalidStep("The step could not be read: " + err.Error())
	}
	if len(def.Steps) != 1 {
		return nil, invalidStep("Give exactly one step to run")
	}
	return def.Steps[0], nil
}

func invalidStep(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
