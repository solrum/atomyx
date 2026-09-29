package runner

import (
	"errors"
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// waitForDefaultTimeoutMs is the poll budget waitFor and
// assertVisible fall back to when a step carries no explicit
// timeoutMs.
const waitForDefaultTimeoutMs = 5000

// WaitForCommand polls for a selector to become visible within a
// timeout, failing the step (not a precondition) when it never does.
type WaitForCommand struct{}

func (WaitForCommand) Command() string { return script.CommandWaitFor }

func (WaitForCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.WaitForStep)
	selector := compileScriptSelector(s.Selector)
	timeoutMs := waitForDefaultTimeoutMs
	if s.TimeoutMs != nil {
		timeoutMs = *s.TimeoutMs
	}

	_, err := ctx.Orchestra.WaitFor(ctx.Context, selector, orchestra.WaitOptions{TimeoutMs: timeoutMs})
	if err != nil {
		var timeoutErr *orchestra.FindTimeoutError
		if errors.As(err, &timeoutErr) {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Element not found within %dms", timeoutMs))}
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr("Element appeared")}
}
