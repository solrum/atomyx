package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// RetryCommand runs its nested steps and, when any of them fails, runs
// the whole list again from its first step, up to the step's maxRetries
// extra attempts. Variables and captures written by an earlier attempt
// stay visible to the next one. The step passes on the first attempt
// that passes. When every attempt fails, the step fails with the last
// attempt's failure.
type RetryCommand struct{}

func (RetryCommand) Command() string { return script.CommandRetry }

func (RetryCommand) runsNestedSteps() {}

func (RetryCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.RetryStep)
	attempts := retryLimit(s) + 1

	var last CommandResult
	for attempt := 1; attempt <= attempts; attempt++ {
		must0(ctx.Context.Err())

		last = ctx.RunSteps(s.Commands)
		if last.Ok {
			return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
				"retry: passed on attempt %d of %d", attempt, attempts))}
		}
		must0(ctx.Context.Err())
	}

	return CommandResult{Ok: false, Code: last.Code, Detail: strPtr(fmt.Sprintf(
		"retry: attempt %d of %d failed: %s", attempts, attempts, detailOrEmpty(last.Detail)))}
}

// retryLimit is the number of extra attempts s allows after the first.
func retryLimit(s *script.RetryStep) int {
	if s.MaxRetries == nil {
		return script.DefaultRetryMaxRetries
	}
	return *s.MaxRetries
}
