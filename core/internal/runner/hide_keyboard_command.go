package runner

import "dev.atomyx/internal/script"

// HideKeyboardCommand dismisses the on-screen keyboard. It passes when
// no keyboard is showing.
type HideKeyboardCommand struct{}

func (HideKeyboardCommand) Command() string { return script.CommandHideKeyboard }

func (HideKeyboardCommand) Execute(_ script.ScriptStep, ctx *CommandContext) CommandResult {
	result := must(ctx.Orchestra.HideKeyboard(ctx.Context))
	if !result.Ok {
		return actionFailure(result)
	}
	return CommandResult{Ok: true, Detail: strPtr(result.Detail)}
}
