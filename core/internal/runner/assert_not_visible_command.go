package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// disappearPollIntervalMs is the poll cadence assertNotVisible uses
// while waiting for a selector to stop matching.
const disappearPollIntervalMs = 100

// AssertNotVisibleCommand asserts that a selector is NOT visible: an
// instant check when the step carries no timeout, or a poll up to the
// given timeout waiting for the element to disappear.
type AssertNotVisibleCommand struct{}

func (AssertNotVisibleCommand) Command() string { return script.CommandAssertNotVisible }

func (AssertNotVisibleCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.AssertNotVisibleStep)
	selector := compileScriptSelector(s.Selector)

	if s.TimeoutMs != nil && *s.TimeoutMs != 0 {
		deadline := ctx.Clock.NowMs() + int64(*s.TimeoutMs)
		for ctx.Clock.NowMs() < deadline {
			cursor := must(ctx.Orchestra.FindOne(ctx.Context, selector))
			if cursor == nil {
				return CommandResult{Ok: true, Detail: strPtr("Element is not visible")}
			}
			must0(ctx.Clock.Sleep(ctx.Context, disappearPollIntervalMs))
		}
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Element still visible after %dms", *s.TimeoutMs))}
	}

	cursor := must(ctx.Orchestra.FindOne(ctx.Context, selector))
	if cursor != nil {
		return CommandResult{Ok: false, Detail: strPtr("Element is still visible (expected not visible)")}
	}
	return CommandResult{Ok: true, Detail: strPtr("Element is not visible")}
}
