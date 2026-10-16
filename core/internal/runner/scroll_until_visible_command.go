package runner

import (
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// defaultScrollDirection is the direction a scrollUntilVisible step
// searches first when it names none.
const defaultScrollDirection = orchestra.ScrollDirectionDown

// ScrollUntilVisibleCommand scrolls until the step's element is in the
// viewport.
type ScrollUntilVisibleCommand struct{}

func (ScrollUntilVisibleCommand) Command() string { return script.CommandScrollUntilVisible }

func (ScrollUntilVisibleCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.ScrollUntilVisibleStep)
	direction := defaultScrollDirection
	if s.Direction != nil {
		direction = *s.Direction
	}
	result := must(ctx.Orchestra.ScrollUntilVisible(ctx.Context, compileScriptSelector(s.Selector),
		orchestra.ScrollUntilVisibleOptions{Direction: direction}))
	if !result.Ok {
		return actionFailure(result)
	}
	return CommandResult{Ok: true, Detail: strPtr(result.Detail)}
}
