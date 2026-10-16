package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// SetOrientationCommand rotates the device to the step's orientation.
// The step succeeds once the screen reports the rotated layout. The
// rotation persists after the script, so a script that rotates the
// device turns it back before it ends.
type SetOrientationCommand struct{}

func (SetOrientationCommand) Command() string { return script.CommandSetOrientation }

func (SetOrientationCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.SetOrientationStep)
	if err := ctx.Orchestra.SetOrientation(ctx.Context, s.Orientation); err != nil {
		if result, ok := commandFailureFor(err); ok {
			return result
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Rotated to %s", s.Orientation))}
}
