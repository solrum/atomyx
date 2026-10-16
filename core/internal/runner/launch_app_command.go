package runner

import (
	"fmt"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// LaunchAppCommand launches the app the running script declares via
// its appId field.
type LaunchAppCommand struct{}

func (LaunchAppCommand) Command() string { return script.CommandLaunchApp }

// launchPolicy is how one launchApp step starts the app: the noReset
// tri-state handed to the launch, and whether the app's data is cleared
// first.
type launchPolicy struct {
	noReset   *bool
	clearData bool
}

// resolveLaunchPolicy combines a step's own options with the run's
// configured launch policy. An option the step sets wins over the
// configuration, and an option it leaves unset falls back to it.
//
// A data clear only makes sense ahead of a fresh (non-noReset) launch:
// attach reuses a running instance, and a plain launch makes no promise
// about process state at all. Asking the step to clear state therefore
// also makes the launch fresh.
func resolveLaunchPolicy(s *script.LaunchAppStep, ctx *CommandContext) launchPolicy {
	noReset := ctx.LaunchNoReset
	if s.StopApp != nil {
		reuse := !*s.StopApp
		noReset = &reuse
	}
	clearData := ctx.ClearDataOnFresh
	if s.ClearState != nil {
		clearData = *s.ClearState
		if clearData {
			fresh := false
			noReset = &fresh
		}
	}
	return launchPolicy{noReset: noReset, clearData: clearData && noReset != nil && !*noReset}
}

// shouldSkip mirrors the dedup condition Orchestra.LaunchApp applies
// internally: a default or attach launch (noReset nil or true) for an
// appId already launched earlier in this flow is a guaranteed no-op, so
// the runner omits the step rather than executing and reporting a
// redundant success. An explicit fresh launch (noReset false) always
// force-stops and relaunches, so it is never skipped, a launch that
// carries arguments or permissions is never skipped either, and an empty
// appId is left to Execute to report as the configuration failure it
// is.
func (LaunchAppCommand) shouldSkip(step script.ScriptStep, ctx *CommandContext) bool {
	appID := ctx.Script.AppID
	if appID == "" {
		return false
	}
	launch := step.(*script.LaunchAppStep)
	policy := resolveLaunchPolicy(launch, ctx)
	dedup := (policy.noReset == nil || *policy.noReset) && len(launch.Arguments) == 0 && len(launch.Permissions) == 0
	return dedup && ctx.Orchestra.IsLaunched(appID)
}

func (LaunchAppCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	appID := ctx.Script.AppID
	if appID == "" {
		return CommandResult{Ok: false, Detail: strPtr("No appId specified in script config")}
	}
	policy := resolveLaunchPolicy(step.(*script.LaunchAppStep), ctx)

	if policy.clearData {
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
	opts := orchestra.LaunchOptions{
		Mode:        orchestra.LaunchModeFromNoReset(policy.noReset),
		Arguments:   step.(*script.LaunchAppStep).Arguments,
		Permissions: step.(*script.LaunchAppStep).Permissions,
	}
	if err := ctx.Orchestra.LaunchAppWithOptions(ctx.Context, appID, opts); err != nil {
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
