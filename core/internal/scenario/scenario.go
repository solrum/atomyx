// Package scenario runs ordered groups of independent scripts
// (ScenarioRunner) and ordered sets of script or scenario entries
// (SuiteRunner) against a shared Orchestra. Every script or scenario
// entry gets its own fresh runner.ScriptRunner, so preconditions,
// guards, and per-run data isolation stay entirely inside
// runner.ScriptRunner; this package adds no execution logic of its
// own beyond sequencing and result aggregation.
package scenario

import (
	"context"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// ScriptRunStatus is one child script's outcome within a scenario
// run.
type ScriptRunStatus string

const (
	StatusPassed  ScriptRunStatus = "passed"
	StatusFailed  ScriptRunStatus = "failed"
	StatusSkipped ScriptRunStatus = "skipped"
	StatusErrored ScriptRunStatus = "errored"
)

// LoadScript loads a child script's YAML text by path, exactly as
// named in a scenario's `scripts` list. Resolving that path (for
// example against the scenario file's own directory) is the loader's
// responsibility, not ScenarioRunner's.
type LoadScript func(path string) (string, error)

// ScenarioScriptResult is one child script's outcome within a
// scenario run.
type ScenarioScriptResult struct {
	ScriptIndex int                  `json:"scriptIndex"`
	ScriptPath  string               `json:"scriptPath"`
	Status      ScriptRunStatus      `json:"status"`
	Result      *runner.ScriptResult `json:"result,omitempty"`
	Error       *string              `json:"error,omitempty"`
	DurationMs  int64                `json:"durationMs"`
}

// ScenarioResult is the outcome of running a whole
// script.ScenarioDefinition.
type ScenarioResult struct {
	ScenarioName  string                 `json:"scenarioName"`
	Ok            bool                   `json:"ok"`
	Scripts       []ScenarioScriptResult `json:"scripts"`
	TotalScripts  int                    `json:"totalScripts"`
	PassedScripts int                    `json:"passedScripts"`
	DurationMs    int64                  `json:"durationMs"`
}

// ScenarioEventType discriminates a ScenarioEvent's variant, matching
// TypeScript's ScenarioEvent union's own `type` discriminant.
type ScenarioEventType string

const (
	// ScenarioStarted fires once, before the first child script runs.
	ScenarioStarted ScenarioEventType = "scenarioStarted"
	// ScriptStarted fires immediately before a non-skipped child
	// script runs.
	ScriptStarted ScenarioEventType = "scriptStarted"
	// ScriptCompleted fires immediately after a non-skipped child
	// script finishes, whether it passed, failed, or errored.
	ScriptCompleted ScenarioEventType = "scriptCompleted"
	// ScenarioCompleted fires once, after every child script has run
	// or been skipped.
	ScenarioCompleted ScenarioEventType = "scenarioCompleted"
)

// ScenarioEvent is a scenario-level lifecycle notification emitted
// through ScenarioRunnerDeps.OnScenarioEvent. It flattens what is
// conceptually a set of distinct event shapes into one struct, so
// which fields are meaningful depends on Type:
//
//   - ScenarioStarted: ScenarioName, TotalScripts.
//   - ScriptStarted: ScriptIndex, ScriptPath.
//   - ScriptCompleted: ScriptIndex, ScriptPath, Status, DurationMs,
//     FailedAtStep (nil when the child script did not fail on a
//     specific step).
//   - ScenarioCompleted: Ok, TotalScripts, PassedScripts, DurationMs.
//
// A skipped child script (see Run's onFailure: "stop" behavior)
// produces neither a ScriptStarted nor a ScriptCompleted event.
type ScenarioEvent struct {
	Type          ScenarioEventType
	ScenarioName  string
	TotalScripts  int
	ScriptIndex   int
	ScriptPath    string
	Status        ScriptRunStatus
	DurationMs    int64
	FailedAtStep  *int
	Ok            bool
	PassedScripts int
}

// ScenarioRunnerDeps constructs a ScenarioRunner. Orchestra, Clock,
// Logger, and LoadScript are required; every other field is optional
// and, when set, is forwarded unchanged to the fresh
// runner.ScriptRunner built for each child script.
type ScenarioRunnerDeps struct {
	Orchestra      *orchestra.Orchestra
	Clock          orchestra.Clock
	Logger         logging.Logger
	NetworkCapture runner.NetworkCapture
	Commands       []runner.CommandDefinition
	// RunID identifies this scenario run for log correlation. It is
	// forwarded unchanged to every child runner.ScriptRunner this
	// ScenarioRunner constructs, so every one of that child's
	// "script.step" log entries carries it under the "runId" field
	// exactly as ScriptRunnerDeps.RunID documents, and it also tags
	// the scenario-level outcome entry this ScenarioRunner logs on
	// Run's completion. Empty omits the field from both.
	RunID string
	// LoadScript resolves each entry of ScenarioDefinition.Scripts to
	// its YAML source.
	LoadScript LoadScript
	// ExternalEnv is merged over the scenario's own `env` block,
	// winning per key, before being passed to every child script's
	// own parse: it lets a host (for example a CLI's --env flag)
	// override a value the scenario pins for every child script.
	ExternalEnv map[string]string
	// OnStep, when non-nil, is forwarded to every child
	// runner.ScriptRunner this ScenarioRunner constructs.
	OnStep                func(runner.StepEvent)
	LaunchNoReset         *bool
	ClearDataOnFresh      bool
	AppArtifact           *string
	PreconditionsOverride runner.PreconditionsOverride
	// OnScenarioEvent, when non-nil, is invoked for every scenario-level
	// lifecycle event this ScenarioRunner emits.
	OnScenarioEvent func(ScenarioEvent)
}

// emit invokes the configured OnScenarioEvent hook with event, doing
// nothing when no hook is configured. A panic from the hook is
// recovered and discarded so a misbehaving consumer callback can
// never crash the scenario run it was only meant to observe.
func (r *ScenarioRunner) emit(event ScenarioEvent) {
	hook := r.deps.OnScenarioEvent
	if hook == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	hook(event)
}

// ScenarioRunner runs an ordered group of independent scripts as one
// logical unit. Each script gets its own fresh runner.ScriptRunner so
// variables, captures, and precondition dedup never leak across
// scripts.
type ScenarioRunner struct {
	deps ScenarioRunnerDeps
	// runLog is deps.Logger extended with deps.RunID, computed once so
	// the scenario-level outcome entry Run logs shares the same
	// correlation field as every child script's own step trace,
	// without rebuilding it on every call.
	runLog logging.Logger
}

// NewScenarioRunner constructs a ScenarioRunner from deps.
func NewScenarioRunner(deps ScenarioRunnerDeps) *ScenarioRunner {
	runLog := deps.Logger
	if deps.RunID != "" {
		runLog = deps.Logger.Child(logging.LogContext{"runId": deps.RunID})
	}
	return &ScenarioRunner{deps: deps, runLog: runLog}
}

// Run executes def.Scripts in order. def.OnFailure (default "stop")
// governs what happens after a script that does not pass: "stop"
// skips every remaining script (recorded with status "skipped" and
// zero duration, without loading or running them); "continue" runs
// every script regardless of earlier failures, so a full regression
// sweep reports every script's outcome in one pass.
//
// It returns a non-nil error only when ctx is canceled between
// scripts; every other failure (a child script that fails, or whose
// YAML cannot be loaded or parsed) is reported through the returned
// ScenarioResult's per-script "failed" or "errored" status, never
// through the error.
func (r *ScenarioRunner) Run(ctx context.Context, def *script.ScenarioDefinition) (ScenarioResult, error) {
	startedAt := r.deps.Clock.NowMs()

	onFailure := "stop"
	if def.OnFailure != nil {
		onFailure = *def.OnFailure
	}

	childEnv := make(map[string]string, len(def.Env)+len(r.deps.ExternalEnv))
	for k, v := range def.Env {
		childEnv[k] = v
	}
	for k, v := range r.deps.ExternalEnv {
		childEnv[k] = v
	}

	total := len(def.Scripts)
	results := make([]ScenarioScriptResult, 0, total)
	stopRequested := false

	r.emit(ScenarioEvent{Type: ScenarioStarted, ScenarioName: def.Name, TotalScripts: total})

	for i, scriptPath := range def.Scripts {
		if err := ctx.Err(); err != nil {
			return ScenarioResult{}, err
		}

		if stopRequested {
			results = append(results, ScenarioScriptResult{
				ScriptIndex: i,
				ScriptPath:  scriptPath,
				Status:      StatusSkipped,
				DurationMs:  0,
			})
			continue
		}

		r.emit(ScenarioEvent{Type: ScriptStarted, ScriptIndex: i, ScriptPath: scriptPath})

		scriptStart := r.deps.Clock.NowMs()
		status, result, errMsg := r.runOne(ctx, scriptPath, childEnv)
		durationMs := r.deps.Clock.NowMs() - scriptStart

		var failedAtStep *int
		if result != nil {
			failedAtStep = result.FailedAtStep
		}

		results = append(results, ScenarioScriptResult{
			ScriptIndex: i,
			ScriptPath:  scriptPath,
			Status:      status,
			Result:      result,
			Error:       errMsg,
			DurationMs:  durationMs,
		})

		r.emit(ScenarioEvent{
			Type:         ScriptCompleted,
			ScriptIndex:  i,
			ScriptPath:   scriptPath,
			Status:       status,
			DurationMs:   durationMs,
			FailedAtStep: failedAtStep,
		})

		if status != StatusPassed && onFailure == "stop" {
			stopRequested = true
		}
	}

	passedScripts := 0
	ok := true
	for _, s := range results {
		if s.Status == StatusPassed {
			passedScripts++
		} else {
			ok = false
		}
	}

	durationMs := r.deps.Clock.NowMs() - startedAt
	r.emit(ScenarioEvent{
		Type:          ScenarioCompleted,
		Ok:            ok,
		TotalScripts:  total,
		PassedScripts: passedScripts,
		DurationMs:    durationMs,
	})

	r.runLog.Info("scenario.run", logging.LogContext{
		"name":       def.Name,
		"ok":         ok,
		"durationMs": durationMs,
	})

	return ScenarioResult{
		ScenarioName:  def.Name,
		Ok:            ok,
		Scripts:       results,
		TotalScripts:  total,
		PassedScripts: passedScripts,
		DurationMs:    durationMs,
	}, nil
}

// runOne loads, parses, and runs one child script against a fresh
// runner.ScriptRunner. A load or parse failure, or a Go error from
// the child runner's own Run (a required flow that could not be
// loaded, see runner.ScriptRunner.Run), is reported as "errored"
// rather than returned as a Go error, so Run's per-script loop can
// record it, decide whether to keep going, and move to the next
// script exactly as it would for a script that ran and failed.
func (r *ScenarioRunner) runOne(ctx context.Context, scriptPath string, childEnv map[string]string) (ScriptRunStatus, *runner.ScriptResult, *string) {
	yaml, err := r.deps.LoadScript(scriptPath)
	if err != nil {
		msg := err.Error()
		return StatusErrored, nil, &msg
	}
	def, err := script.ParseScript(yaml, childEnv)
	if err != nil {
		msg := err.Error()
		return StatusErrored, nil, &msg
	}

	childRunner := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:             r.deps.Orchestra,
		Clock:                 r.deps.Clock,
		Logger:                r.deps.Logger,
		RunID:                 r.deps.RunID,
		NetworkCapture:        r.deps.NetworkCapture,
		Commands:              r.deps.Commands,
		OnStep:                r.deps.OnStep,
		LaunchNoReset:         r.deps.LaunchNoReset,
		ClearDataOnFresh:      r.deps.ClearDataOnFresh,
		AppArtifact:           r.deps.AppArtifact,
		PreconditionsOverride: r.deps.PreconditionsOverride,
	})

	result, err := childRunner.Run(ctx, def)
	if err != nil {
		msg := err.Error()
		return StatusErrored, nil, &msg
	}
	if result.Ok {
		return StatusPassed, &result, nil
	}
	return StatusFailed, &result, nil
}

// MemoryScriptLoader returns a LoadScript backed by a fixed map of
// path to YAML source, for hosts and tests that already have every
// child script's content in memory. It errors on a path scripts does
// not contain.
func MemoryScriptLoader(scripts map[string]string) LoadScript {
	return func(path string) (string, error) {
		yaml, ok := scripts[path]
		if !ok {
			return "", &unknownScriptError{path: path}
		}
		return yaml, nil
	}
}

type unknownScriptError struct{ path string }

func (e *unknownScriptError) Error() string {
	return "scenario referenced unknown script: " + e.path
}
