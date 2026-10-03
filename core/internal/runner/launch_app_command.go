package runner

import (
	"fmt"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// LaunchAppCommand launches the app the running script declares via
// its appId field.
type LaunchAppCommand struct{}

func (LaunchAppCommand) Command() string { return script.CommandLaunchApp }

// shouldSkip mirrors the dedup condition Orchestra.LaunchApp applies
// internally: a default or attach launch (LaunchNoReset nil or true)
// for an appId already launched earlier in this flow is a guaranteed
// no-op, so the runner omits the step rather than executing and
// reporting a redundant success. An explicit fresh launch
// (LaunchNoReset false) always force-stops and relaunches, so it is
// never skipped, and an empty appId is left to Execute to report as
// the configuration failure it is.
func (LaunchAppCommand) shouldSkip(ctx *CommandContext) bool {
	appID := ctx.Script.AppID
	if appID == "" {
		return false
	}
	dedup := ctx.LaunchNoReset == nil || *ctx.LaunchNoReset
	return dedup && ctx.Orchestra.IsLaunched(appID)
}

func (LaunchAppCommand) Execute(_ script.ScriptStep, ctx *CommandContext) CommandResult {
	appID := ctx.Script.AppID
	if appID == "" {
		return CommandResult{Ok: false, Detail: strPtr("No appId specified in script config")}
	}

	// A clean-data reset only makes sense ahead of a fresh
	// (non-noReset) launch: attach reuses a running instance, and a
	// plain launch makes no promise about process state at all.
	if ctx.LaunchNoReset != nil && !*ctx.LaunchNoReset && ctx.ClearDataOnFresh {
		source := ""
		if ctx.AppArtifact != nil {
			source = *ctx.AppArtifact
		}
		if err := ctx.Orchestra.ClearAppData(ctx.Context, appID, driver.ClearAppDataOptions{Source: source}); err != nil {
			if result, ok := commandFailureFor(err); ok {
				return withAuthoredAppID(result, ctx)
			}
			panic(err)
		}
	}

	// An app id that is well-formed but not installed on the device is
	// a normal, expected outcome of running the script against the
	// wrong target, not a bug in this handler, so it fails the step
	// like any other failing command instead of propagating as a panic.
	if err := ctx.Orchestra.LaunchApp(ctx.Context, appID, driver.LaunchArgs{NoReset: ctx.LaunchNoReset}); err != nil {
		if result, ok := commandFailureFor(err); ok {
			return withAuthoredAppID(result, ctx)
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Launched %s", ctx.Script.AuthoredAppID()))}
}

// withAuthoredAppID rewrites a launch failure so it names the app id as
// the script author wrote it, not the value a variable expanded to.
func withAuthoredAppID(result CommandResult, ctx *CommandContext) CommandResult {
	result.Detail = strPtr(restoreAuthored(detailOrEmpty(result.Detail), ctx.Script.AppID, ctx.Script.AuthoredAppID()))
	return result
}
