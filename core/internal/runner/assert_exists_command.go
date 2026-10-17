package runner

import (
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// AssertExistsCommand asserts that a selector matches a node in the
// UI hierarchy: an instant check when the step carries no timeout, or
// a poll up to the given timeout otherwise.
type AssertExistsCommand struct{}

func (AssertExistsCommand) Command() string { return script.CommandAssertExists }

func (AssertExistsCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.AssertExistsStep)
	selector := compileScriptSelector(s.Selector)

	if s.TimeoutMs != nil && *s.TimeoutMs != 0 {
		_, err := ctx.Orchestra.WaitFor(ctx.Context, selector, orchestra.WaitOptions{TimeoutMs: *s.TimeoutMs})
		if err != nil {
			return pollFailure(err, fmt.Sprintf("Element not found within %dms", *s.TimeoutMs))
		}
		return CommandResult{Ok: true, Detail: strPtr("Element exists")}
	}

	cursor := must(ctx.Orchestra.FindOne(ctx.Context, selector))
	if cursor == nil {
		return CommandResult{Ok: false, Detail: strPtr("Element not found")}
	}
	return CommandResult{Ok: true, Detail: strPtr("Element exists")}
}
