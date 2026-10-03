package runner

import (
	"errors"
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
	written := authored[*script.PressKeyStep](step)
	result, err := ctx.Orchestra.PressKey(ctx.Context, driver.KeyCode(s.Key))
	if err != nil {
		// The driver names the key it was given, which is the value a
		// placeholder expanded to, so the failure is re-raised with the
		// key as the author wrote it.
		panic(errors.New(restoreAuthored(err.Error(), s.Key, written.Key)))
	}
	if !result.Ok {
		return CommandResult{Ok: false, Detail: strPtr(restoreAuthored(result.Reason, s.Key, written.Key))}
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Pressed %s", written.Key))}
}
