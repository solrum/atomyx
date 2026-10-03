package runner

import (
	"fmt"

	"dev.atomyx/internal/script"
)

// captureTimeoutMs is the fixed budget capture waits for a matching
// request before failing the step.
const captureTimeoutMs = 10_000

// CaptureCommand arms the network-capture proxy for the next request
// matching the step's pattern and binds it to a variable.
type CaptureCommand struct{}

func (CaptureCommand) Command() string { return script.CommandCapture }

func (CaptureCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.CaptureStep)
	written := authored[*script.CaptureStep](step)

	if ctx.NetworkCapture == nil {
		return CommandResult{Ok: false, Detail: strPtr(
			"No network capture adapter configured. Pass --proxy to the CLI " +
				"or set captureConfig in runner options.")}
	}

	captured, err := ctx.NetworkCapture.WaitForRequest(ctx.Context, s.Pattern, captureTimeoutMs)
	if err != nil {
		return CommandResult{Ok: false, Detail: strPtr(restoreAuthored(err.Error(), s.Pattern, written.Pattern))}
	}
	ctx.Captures[s.As] = captured
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
		"Captured %s %s returning status %d as \"%s\"", captured.Method, captured.URL, captured.Status, written.As))}
}
