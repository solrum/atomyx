package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// EraseTextCommand erases characters backwards from the focused field.
type EraseTextCommand struct{}

func (EraseTextCommand) Command() string { return script.CommandEraseText }

func (EraseTextCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.EraseTextStep)
	count := script.DefaultEraseTextCharacters
	if s.Characters != nil {
		count = *s.Characters
	}
	if !ctx.Orchestra.Capabilities().CanEraseText {
		return CommandResult{Ok: false, Detail: strPtr("This device cannot erase text directly. Use `pressKey: delete` instead.")}
	}
	must0(ctx.Orchestra.EraseText(ctx.Context, count))
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Erased up to %d characters", count))}
}
