package runner

import "dev.atomyx/internal/script"

// StopAppCommand stops the step's app, or the script's app when the
// step names none.
type StopAppCommand struct{}

func (StopAppCommand) Command() string { return script.CommandStopApp }

func (StopAppCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.StopAppStep)
	written := authored[*script.StopAppStep](step)
	return runAppAction(s.AppID, written.AppID, "Stopped", ctx, func(appID string) error {
		return ctx.Orchestra.StopApp(ctx.Context, appID)
	})
}
