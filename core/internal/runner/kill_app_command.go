package runner

import "dev.atomyx/internal/script"

// KillAppCommand terminates the step's app, or the script's app when
// the step names none. Every platform adapter performs the same device
// operation for a kill as for a stop.
type KillAppCommand struct{}

func (KillAppCommand) Command() string { return script.CommandKillApp }

func (KillAppCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.KillAppStep)
	written := authored[*script.KillAppStep](step)
	return runAppAction(s.AppID, written.AppID, "Killed", ctx, func(appID string) error {
		return ctx.Orchestra.KillApp(ctx.Context, appID)
	})
}
