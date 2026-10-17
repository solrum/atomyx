package runner

import (
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// AssertVisibleCommand asserts that a selector is visible: an instant
// check when the step carries no timeout, or a poll up to the given
// timeout otherwise.
type AssertVisibleCommand struct{}

func (AssertVisibleCommand) Command() string { return script.CommandAssertVisible }

func (AssertVisibleCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.AssertVisibleStep)
	selector := compileScriptSelector(s.Selector)

	if s.TimeoutMs != nil && *s.TimeoutMs != 0 {
		_, err := ctx.Orchestra.WaitFor(ctx.Context, selector, orchestra.WaitOptions{TimeoutMs: *s.TimeoutMs})
		if err != nil {
			return pollFailure(err, fmt.Sprintf("Element not visible within %dms", *s.TimeoutMs))
		}
		return CommandResult{Ok: true, Detail: strPtr("Element is visible")}
	}

	cursor := must(ctx.Orchestra.FindOne(ctx.Context, selector))
	if cursor == nil {
		return CommandResult{Ok: false, Detail: strPtr("Element not visible on screen")}
	}
	return CommandResult{Ok: true, Detail: strPtr("Element is visible")}
}
