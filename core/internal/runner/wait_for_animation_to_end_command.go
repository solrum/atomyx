package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// WaitForAnimationToEndCommand waits until the screen stops changing.
// A screen that is still changing when the timeout elapses does not fail
// the step, because a screen with a permanent animation, such as a
// spinner, never settles and the next step still has to run.
type WaitForAnimationToEndCommand struct{}

func (WaitForAnimationToEndCommand) Command() string { return script.CommandWaitForAnimationToEnd }

func (WaitForAnimationToEndCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.WaitForAnimationToEndStep)
	timeoutMs := script.DefaultAnimationTimeoutMs
	if s.TimeoutMs != nil {
		timeoutMs = *s.TimeoutMs
	}
	startedAt := ctx.Clock.NowMs()
	settled := must(ctx.Orchestra.WaitForIdle(ctx.Context, timeoutMs))
	waited := ctx.Clock.NowMs() - startedAt
	if settled {
		return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Screen settled after %dms", waited))}
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Screen still changing after %dms, continuing", waited))}
}
