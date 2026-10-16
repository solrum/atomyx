package runner

import "fmt"

// noAppIDDetail is the failure detail of an app step that names no app
// while the script declares none either.
const noAppIDDetail = "No appId specified in the step or in the script config"

// runAppAction runs act against the app a lifecycle step targets: the
// step's own app id when it sets one, otherwise the script's. verb is
// the past-tense word the success detail leads with.
//
// A failure the device reports with a stable code, such as an invalid
// app id, fails the step. Any other error is unexpected and propagates
// as a panic for the runner to recover. Every detail names the app id
// as the script author wrote it, so a value a `${var}` placeholder
// expanded to never reaches a step record.
func runAppAction(stepAppID, writtenAppID *string, verb string, ctx *CommandContext, act func(appID string) error) CommandResult {
	appID, written := ctx.Script.AppID, ctx.Script.AuthoredAppID()
	if stepAppID != nil {
		appID, written = *stepAppID, *writtenAppID
	}
	if appID == "" {
		return CommandResult{Ok: false, Detail: strPtr(noAppIDDetail)}
	}
	if err := act(appID); err != nil {
		if result, ok := commandFailureFor(err); ok {
			result.Detail = strPtr(restoreAuthored(detailOrEmpty(result.Detail), appID, written))
			return result
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("%s %s", verb, written))}
}
