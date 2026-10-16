package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// OpenLinkCommand opens the step's link in the device's handler for it,
// or in the step's app when it names one. The step succeeds once the
// device accepts the link, so a script that needs the destination follows
// it with a wait for an element on that screen.
type OpenLinkCommand struct{}

func (OpenLinkCommand) Command() string { return script.CommandOpenLink }

func (OpenLinkCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.OpenLinkStep)
	written := authored[*script.OpenLinkStep](step)
	appID := ""
	if s.AppID != nil {
		appID = *s.AppID
	}
	if err := ctx.Orchestra.OpenLink(ctx.Context, s.Link, appID); err != nil {
		if result, ok := commandFailureFor(err); ok {
			result.Detail = strPtr(restoreAuthored(detailOrEmpty(result.Detail), s.Link, written.Link))
			return result
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Opened %s", written.Link))}
}
