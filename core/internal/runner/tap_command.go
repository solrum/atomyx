package runner

import (
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// TapCommand taps the element resolved by a TapStep's selector.
type TapCommand struct{}

func (TapCommand) Command() string { return script.CommandTap }

func (TapCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.TapStep)
	selector := compileScriptSelector(s.Selector)
	result := must(ctx.Orchestra.Tap(ctx.Context, selector, orchestra.TapOptions{}))
	if !result.Ok {
		return actionFailure(result)
	}
	return CommandResult{Ok: true, Detail: strPtr(result.Detail)}
}
