package report

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// unknownFailureText is the failure text of a run that failed without a
// failing step or a message to name.
const unknownFailureText = "failed"

// scriptFailureText names the failing step of a script result and its
// detail, followed by the reason the run aborted when it did.
func scriptFailureText(result runner.ScriptResult) string {
	var parts []string
	for _, step := range result.Steps {
		if step.Ok {
			continue
		}
		text := fmt.Sprintf("step %d (%s) failed", step.StepIndex+1, step.Command)
		if step.Detail != nil && *step.Detail != "" {
			text += ": " + *step.Detail
		}
		parts = append(parts, text)
		break
	}
	if len(parts) == 0 && result.FailedAtStep != nil {
		parts = append(parts, fmt.Sprintf("step %d failed", *result.FailedAtStep+1))
	}
	if result.Abort != nil && result.Abort.Message != "" {
		parts = append(parts, "aborted: "+result.Abort.Message)
	}
	if len(parts) == 0 {
		return unknownFailureText
	}
	return strings.Join(parts, ", ")
}

// scenarioFailureText names the first child script that did not pass and
// why.
func scenarioFailureText(result scenario.ScenarioResult) string {
	for _, child := range result.Scripts {
		if child.Status != scenario.StatusFailed && child.Status != scenario.StatusErrored {
			continue
		}
		reason := unknownFailureText
		switch {
		case child.Error != nil:
			reason = *child.Error
		case child.Result != nil:
			reason = scriptFailureText(*child.Result)
		}
		return fmt.Sprintf("script %s: %s", child.ScriptPath, reason)
	}
	return unknownFailureText
}

// AttemptFailureText describes why an attempt failed. ran reports whether
// the entry produced a result at all, which separates a failed test from
// an entry that could not run, such as one that fails to load.
func AttemptFailureText(attempt scenario.SuiteAttempt) (text string, ran bool) {
	if attempt.Failure != "" {
		return attempt.Failure, true
	}
	if attempt.Result != nil {
		switch {
		case attempt.Result.Script != nil:
			return scriptFailureText(*attempt.Result.Script), true
		case attempt.Result.Scenario != nil:
			return scenarioFailureText(*attempt.Result.Scenario), true
		}
	}
	if attempt.Error != nil {
		return *attempt.Error, false
	}
	return unknownFailureText, false
}
