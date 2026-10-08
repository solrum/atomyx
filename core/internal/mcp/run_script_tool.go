package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var runScriptInputSchema = jsObject(map[string]any{
	"script": jsStringDesc("YAML test script content. Use the Atomyx YAML format with " +
		"appId, name, env, and steps."),
	"env": jsStringRecord(),
	"proxy": jsStringDesc("Network capture target for a script whose proxy field is " +
		"\"required\", in \"type:path\" form, for example \"file:/tmp/capture.jsonl\". " +
		"Omit when the script declares no proxy requirement."),
}, "script")

var runScriptSchema = compileToolSchema("run_script", runScriptInputSchema)

type runScriptArgs struct {
	Script string            `json:"script"`
	Env    map[string]string `json:"env,omitempty"`
	Proxy  string            `json:"proxy,omitempty"`
}

// stepResultWire and scriptResultWire are the wire shapes returned by
// run_script. Artifacts always serializes as an empty object: it is
// reserved for future artifact metadata, so a client must treat
// "artifacts": {} as the stable response shape regardless of how many
// screenshots a run actually captured.
type stepResultWire struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
	// DeviceLost is true only for the step the run aborted on when
	// that abort was caused by the device or agent going unreachable,
	// so a caller can tell that apart from an ordinary failed
	// assertion or command without parsing Detail. Omitted for every
	// other step.
	DeviceLost bool `json:"deviceLost,omitempty"`
}

type scriptResultWire struct {
	ScriptName   string           `json:"scriptName"`
	Ok           bool             `json:"ok"`
	Steps        []stepResultWire `json:"steps"`
	TotalSteps   int              `json:"totalSteps"`
	PassedSteps  int              `json:"passedSteps"`
	FailedAtStep *int             `json:"failedAtStep,omitempty"`
	DurationMs   int64            `json:"durationMs"`
	Artifacts    struct{}         `json:"artifacts"`
	// DeviceLost is true when the run stopped early because the
	// device or agent went unreachable, as opposed to running to
	// completion or stopping on an ordinary failed step. Omitted when
	// false.
	DeviceLost bool `json:"deviceLost,omitempty"`
	// Abort is present only when the run stopped early for a reason
	// beyond an ordinary step failure, consistent with how
	// runner.ScriptResult.Abort reports the same outcome. DeviceLost
	// above covers one such reason with its own dedicated field for
	// backward compatibility; Abort carries every reason, including
	// one a caller has not seen before, without requiring a new
	// boolean field each time.
	Abort *abortWire `json:"abort,omitempty"`
}

// abortWire is the run_script result's shape for runner.RunAbort.
type abortWire struct {
	Reason  string `json:"reason"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Remedy  string `json:"remedy,omitempty"`
}

func toScriptResultWire(result runner.ScriptResult) scriptResultWire {
	deviceLost := result.Abort != nil && result.Abort.Reason == runner.RunAbortDeviceLost
	steps := make([]stepResultWire, len(result.Steps))
	for i, s := range result.Steps {
		steps[i] = stepResultWire{
			StepIndex:  s.StepIndex,
			Command:    s.Command,
			Ok:         s.Ok,
			Detail:     s.Detail,
			DurationMs: s.DurationMs,
			DeviceLost: deviceLost && result.FailedAtStep != nil && s.StepIndex == *result.FailedAtStep,
		}
	}
	var abort *abortWire
	if result.Abort != nil {
		abort = &abortWire{
			Reason:  string(result.Abort.Reason),
			Code:    result.Abort.Code,
			Message: result.Abort.Message,
			Remedy:  result.Abort.Remedy,
		}
	}
	return scriptResultWire{
		ScriptName:   result.ScriptName,
		Ok:           result.Ok,
		Steps:        steps,
		TotalSteps:   result.TotalSteps,
		PassedSteps:  result.PassedSteps,
		FailedAtStep: result.FailedAtStep,
		DurationMs:   result.DurationMs,
		DeviceLost:   deviceLost,
		Abort:        abort,
	}
}

// registerRunScriptTool adds the run_script tool: parses a YAML
// script and executes it deterministically against the active
// device, step by step, stopping on the first failure. The script
// writes into the open run when there is one, and is its own run
// otherwise.
func registerRunScriptTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "run_script",
		Description: "Execute a YAML test script deterministically against the current device. " +
			"Steps run in order, and execution stops on the first failure. " +
			"Returns structured results with a pass or fail outcome per step. When a " +
			"failure happened because the device or agent went unreachable rather than " +
			"an ordinary assertion or command failure, the failed step and the top " +
			"level result both carry deviceLost true. A script whose proxy field is " +
			"\"required\" needs a proxy argument on this call; without one the result " +
			"carries ok false, zero steps, and a top level abort object naming why. " +
			"Use this for regression runs or replaying recorded flows, not for " +
			"exploratory testing. Inside a run opened by start_run, the script's " +
			"screenshots go into that run. With no run open, the call is its own " +
			"run, finalized when the script ends.",
		InputSchema: runScriptInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(runScriptSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("run_script", err), nil
		}
		var args runScriptArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("run_script", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		def, err := script.ParseScript(args.Script, args.Env)
		if err != nil {
			return errorResult(err), nil
		}
		networkCapture, err := netcapture.New(netcapture.ParseConfigString(args.Proxy))
		if err != nil {
			return errorResult(err), nil
		}

		result, err := tc.RunSession.RunScript(ctx, def.Name, tc.Session.SelectedDescriptor(),
			func(target scriptTarget) (runner.ScriptResult, error) {
				var gate runner.StepGate
				dbg, endDebug := tc.Session.beginScriptDebug(target.Ctx, orch, def)
				defer endDebug()
				if dbg != nil {
					gate = dbg
				}
				result, err := runner.NewScriptRunner(runner.ScriptRunnerDeps{
					StepGate:       gate,
					Orchestra:      orch,
					Clock:          tc.Clock,
					Logger:         tc.Logger,
					RunID:          target.RunID,
					Commands:       runner.DefaultCommands(),
					NetworkCapture: networkCapture,
					Artifacts:      target.Artifacts,
					OnStep:         target.OnStep,
					FailureCapture: runner.NewFailureBundle(runner.FailureBundleDeps{
						Orchestra: orch, Clock: tc.Clock, Logger: tc.Logger, Sink: target.Evidence,
					}),
				}).Run(target.Ctx, def)
				if err != nil && dbg != nil && dbg.endedByDeviceLoss() {
					return deviceLostResult(def.Name), nil
				}
				return result, err
			})
		if err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(toScriptResultWire(result))
	})
}
