package runner

import (
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// ClearStateCommand resets the step's app, or the script's app when
// the step names none, to a clean data state. When the run configures
// an installable artifact for the app, it is passed along for the
// platforms that reach a clean state by reinstalling.
type ClearStateCommand struct{}

func (ClearStateCommand) Command() string { return script.CommandClearState }

func (ClearStateCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.ClearStateStep)
	written := authored[*script.ClearStateStep](step)
	source := ""
	if ctx.AppArtifact != nil {
		source = *ctx.AppArtifact
	}
	return runAppAction(s.AppID, written.AppID, "Cleared state of", ctx, func(appID string) error {
		return ctx.Orchestra.ClearAppData(ctx.Context, appID, driver.ClearAppDataOptions{Source: source})
	})
}
