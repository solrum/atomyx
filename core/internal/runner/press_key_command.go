package runner

import (
	"fmt"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// PressKeyCommand presses the hardware or system key named by the
// step.
type PressKeyCommand struct{}

func (PressKeyCommand) Command() string { return script.CommandPressKey }

func (PressKeyCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.PressKeyStep)
	result := must(ctx.Orchestra.PressKey(ctx.Context, driver.KeyCode(s.Key)))
	if !result.Ok {
		return CommandResult{Ok: false, Detail: strPtr(result.Reason)}
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Pressed %s", s.Key))}
}
