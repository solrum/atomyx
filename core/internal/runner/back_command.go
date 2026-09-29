package runner

import (
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// BackCommand navigates back one step, via the driver's back key.
type BackCommand struct{}

func (BackCommand) Command() string { return script.CommandBack }

func (BackCommand) Execute(_ script.ScriptStep, ctx *CommandContext) CommandResult {
	result := must(ctx.Orchestra.PressKey(ctx.Context, driver.KeyBack))
	if !result.Ok {
		return CommandResult{Ok: false, Detail: strPtr(result.Reason)}
	}
	return CommandResult{Ok: true, Detail: strPtr("Pressed back")}
}
