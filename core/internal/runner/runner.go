package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// PreconditionsOverride is a run-level enforcement override applied on
// top of every precondition's own policy: PreconditionsSkip suppresses
// every precondition in this run, PreconditionsForce runs every
// precondition's setup unconditionally. PreconditionsNoOverride leaves
// each precondition's own policy (default "ensure") in effect.
type PreconditionsOverride string

const (
	PreconditionsNoOverride PreconditionsOverride = ""
	PreconditionsSkip       PreconditionsOverride = "skip"
	PreconditionsForce      PreconditionsOverride = "force"
)

// ScriptRunnerDeps constructs a ScriptRunner.
type ScriptRunnerDeps struct {
	// Orchestra is required: every command dispatches through it.
	Orchestra *orchestra.Orchestra
	// Clock is required: it times every step and backs every sleep.
	Clock orchestra.Clock
	// Logger is required: commands and the runner itself log through
	// it.
	Logger logging.Logger
	// NetworkCapture is the capture adapter for this run: nil, or a
	// no-op adapter reporting IsNoCaptureConfigured() true, when no
	// proxy is set up. A script whose `proxy` field is "required"
	// fails immediately in either case.
	NetworkCapture NetworkCapture
	// FlowLoader resolves a file-referenced step list (a precondition
	// `setup`, and later a `handle`/`branch` `do` or a `runFlow`
	// target) into a ScriptDefinition. Nil when the host's scripts
	// never reference an external flow file.
	FlowLoader FlowLoader
	// Artifacts collects this run's screenshots and other evidence.
	// Nil constructs a fresh in-memory collector for each Run call, so
	// two Run calls never share collected evidence unless the caller
	// injects its own ScriptArtifacts here.
	Artifacts ScriptArtifacts
	// Commands is the command registry. Nil (or empty) registers no
	// commands, so every step fails with "Unknown command".
	Commands []CommandDefinition
	// LaunchNoReset is the default `noReset` a `launchApp` command
	// passes to Orchestra.LaunchApp when a step carries no override.
	LaunchNoReset *bool
	// ClearDataOnFresh is forwarded to every `launchApp` command as
	// CommandContext.ClearDataOnFresh.
	ClearDataOnFresh bool
	// AppArtifact is forwarded to every `launchApp` command as
	// CommandContext.AppArtifact.
	AppArtifact *string
	// PreconditionsOverride is applied on top of every precondition's
	// own policy for every Run call this ScriptRunner makes.
	PreconditionsOverride PreconditionsOverride
	// OnStep, when non-nil, is invoked before and after every step
	// this runner executes, including the main script's own steps, and every
	// step of a nested step list run through CommandContext.RunSteps
	// (a required flow, a precondition setup, or a
	// handle/branch/runFlow target). Nil emits nothing: Run behaves
	// exactly as it does with the field zero-valued, so a caller that
	// never sets it pays no cost for the hook.
	OnStep func(StepEvent)
	// RunID identifies this ScriptRunner's run for log correlation: the
	// same identifier a report bundle for this run is keyed by, when
	// the host also writes one. Every step-outcome log line this
	// runner emits carries it under the "runId" field. Empty omits the
	// field, which a caller only wants when it has no report id to
	// correlate against.
	RunID string
	// FailureCapture, when non-nil, is called once for a failed
	// top-level step whose failure is an ordinary one. It is not
	// called when the run aborts because the device was lost, or when
	// the run's context is already done. Nil captures nothing.
	FailureCapture FailureCapture
	// SourcePath is the script's source file, absolute when set and
	// empty for an inline script. It only shapes the location a
	// StepGate sees for a top-level step.
	SourcePath string
	// StepGate, when non-nil, is consulted before every step at every
	// depth. Nil gates nothing.
	StepGate StepGate
}

// StepResult is the outcome of one executed top-level step.
type StepResult struct {
	StepIndex  int
	Command    string
	Ok         bool
	Detail     *string
	DurationMs int64
}

// ScriptResult is the outcome of running a whole ScriptDefinition.
type ScriptResult struct {
	ScriptName   string
	Ok           bool
	Steps        []StepResult
	TotalSteps   int
	PassedSteps  int
	FailedAtStep *int
	DurationMs   int64
	Artifacts    ScriptArtifacts
	// Abort is non-nil when the run stopped early for a reason beyond
	// an ordinary step failure, such as the device or agent going
	// unreachable once a step failed. Ok is false whenever Abort is
	// non-nil, and FailedAtStep still names the step that was running
	// when the run stopped. A caller reporting this result must check
	// Abort before treating a false Ok as an ordinary step failure.
	Abort *RunAbort
}

// RunAbortReason names why a run stopped for a reason beyond an
// ordinary step failure.
type RunAbortReason string

const (
	// RunAbortDeviceLost means a step failed and the liveness probe
	// that followed it found the agent unreachable, so the run
	// stopped rather than reporting the failure as an ordinary step
	// outcome.
	RunAbortDeviceLost RunAbortReason = "deviceLost"
	// RunAbortProxyRequired means the script declares `proxy: required`
	// and this run has no network capture adapter configured, so the
	// run was refused before its first step.
	RunAbortProxyRequired RunAbortReason = "proxyRequired"
)

// proxyRequiredMessage and proxyRequiredRemedy are the fixed text a
// `proxy: required` script reports when refused for lack of a
// configured network capture adapter. The remedy names both surfaces
// that can supply one, since the same runner backs the CLI's run
// command and the MCP server's run_script tool.
const (
	proxyRequiredMessage = "This script needs network capture, but no proxy is configured."
	proxyRequiredRemedy  = "Pass --proxy <type:path> on the CLI, or set the \"proxy\" argument on the run_script tool call."
)

// RunAbort carries the details of a run that stopped early for a
// reason beyond an ordinary step failure.
type RunAbort struct {
	// Reason names why the run stopped.
	Reason RunAbortReason
	// Code is the stable diagnostic code describing the underlying
	// fault, for a caller that renders or branches on it.
	Code string
	// Message describes the fault, safe to show as is.
	Message string
	// Remedy is the action a caller can take in response, or empty
	// when the fault carries none.
	Remedy string
}

// ScriptRunner executes a ScriptDefinition step by step against an
// Orchestra instance. It fails fast on the first step that returns
// Ok false.
//
// No LLM, no agent, no branching of its own. Pure sequential
// execution. Branching commands (handle, branch, runFlow) call back
// into RunSteps and RunPreconditions on CommandContext to run nested
// step lists in the same run's shared variables and captures.
type ScriptRunner struct {
	deps     ScriptRunnerDeps
	registry map[string]CommandDefinition
	// stepLog is deps.Logger extended with deps.RunID, computed once
	// so every step-outcome log line this runner emits shares the same
	// correlation field without rebuilding it per step.
	stepLog logging.Logger

	// requiresMu guards completedRequires and callStack, which live
	// for the ScriptRunner's whole lifetime rather than a single Run
	// call: a required flow completed by an earlier Run call is
	// deduped in a later one, so the same ScriptRunner instance never
	// re-runs a required flow's setup twice across separate scripts
	// that both require it.
	requiresMu        sync.Mutex
	completedRequires map[string]struct{}
	callStack         map[string]struct{}

	// trailMu guards trail, the events emitted since the current
	// top-level step started, which a failure capture reports.
	trailMu sync.Mutex
	trail   []StepEvent
}

// NewScriptRunner constructs a ScriptRunner from deps. Orchestra,
// Clock, and Logger must be non-nil.
func NewScriptRunner(deps ScriptRunnerDeps) *ScriptRunner {
	registry := make(map[string]CommandDefinition, len(deps.Commands))
	for _, cmd := range deps.Commands {
		registry[cmd.Command()] = cmd
	}
	stepLog := deps.Logger
	if deps.RunID != "" {
		stepLog = deps.Logger.Child(logging.LogContext{"runId": deps.RunID})
	}
	return &ScriptRunner{
		deps:              deps,
		registry:          registry,
		stepLog:           stepLog,
		completedRequires: make(map[string]struct{}),
		callStack:         make(map[string]struct{}),
	}
}

// runState is the state shared across every step of one Run call: the
// definition being run, its variables and captures, the artifacts
// collector, and the dedup set preconditions share across the main
// script and every nested setup they run.
type runState struct {
	def                         *script.ScriptDefinition
	variables                   map[string]ScriptValue
	captures                    map[string]CapturedRequest
	artifacts                   ScriptArtifacts
	completedPreconditionSetups map[string]struct{}
	// globalStepIndex is a monotonic counter spanning this whole Run
	// call, consumed by every step that actually executes, including main
	// script steps and every nested step list's steps alike, in
	// execution order. It backs StepEvent.StepIndex only; StepResult
	// keeps its own position-within-script-steps index.
	globalStepIndex int
	// depth is the current nesting level, incremented for the
	// duration of each runNestedSteps call and restored after. It
	// backs StepEvent.Depth for events emitted from inside a nested
	// step list; the main step loop always emits depth 0 directly.
	depth int
	// gateErr is the first error a StepGate returned. A nested gate
	// failure surfaces as a failed step, and Run then returns gateErr
	// instead of a script result.
	gateErr error
	// secrets names the variables that hold a secret. It is a side
	// table so the value type stays a plain number, string or boolean.
	secrets map[string]bool
}

// Run executes def step by step. It returns a non-nil error when ctx
// is canceled between steps, or when a required flow named in
// def.Requires cannot be loaded (the file cannot be read, or its
// contents fail to parse): loading a required flow happens before any
// step runs and outside the per-step failure handling every other
// command goes through, so a broken required-flow reference aborts
// the run outright as a Go error rather than reporting a graceful
// failed ScriptResult. Every other script-authoring failure (an
// unknown command, a failed assertion, a panic recovered from a
// command handler, or a required flow whose own preconditions or
// steps fail once it did load) is always reported through the
// returned ScriptResult, never through the error. When the error is
// non-nil the returned ScriptResult is not Ok. It is the zero value
// except when ctx ended or a StepGate returned an error between steps,
// in which case it holds the steps that ran before the stop.
func (r *ScriptRunner) Run(ctx context.Context, def *script.ScriptDefinition) (ScriptResult, error) {
	if r.deps.RunID != "" {
		ctx = logging.WithRunID(ctx, r.deps.RunID)
	}

	startedAt := r.deps.Clock.NowMs()
	artifacts := r.newRunArtifacts()

	if def.Proxy != nil && *def.Proxy == "required" && noCaptureConfigured(r.deps.NetworkCapture) {
		return ScriptResult{
			ScriptName:  def.Name,
			Ok:          false,
			Steps:       []StepResult{},
			TotalSteps:  len(def.Steps),
			PassedSteps: 0,
			DurationMs:  r.deps.Clock.NowMs() - startedAt,
			Artifacts:   artifacts,
			Abort: &RunAbort{
				Reason:  RunAbortProxyRequired,
				Code:    diag.CodeProxyRequired,
				Message: proxyRequiredMessage,
				Remedy:  proxyRequiredRemedy,
			},
		}, nil
	}

	rs := &runState{
		def:                         def,
		variables:                   variablesFromEnv(def.Env),
		secrets:                     secretsFromEnv(def.Env),
		captures:                    make(map[string]CapturedRequest),
		artifacts:                   artifacts,
		completedPreconditionSetups: make(map[string]struct{}),
	}

	failure, err := r.runRequiredFlows(ctx, rs, def.Requires, startedAt, artifacts)
	if rs.gateErr != nil {
		return ScriptResult{}, rs.gateErr
	}
	if err != nil {
		return ScriptResult{}, err
	}
	if failure != nil {
		return *failure, nil
	}

	preconditionResult := r.runPreconditions(ctx, rs, def.Preconditions)
	if rs.gateErr != nil {
		return ScriptResult{}, rs.gateErr
	}
	if !preconditionResult.Ok {
		return ScriptResult{
			ScriptName: def.Name,
			Ok:         false,
			Steps: []StepResult{{
				StepIndex: -1,
				Command:   "preconditions",
				Ok:        false,
				Detail:    preconditionResult.Detail,
			}},
			TotalSteps:   len(def.Steps),
			PassedSteps:  0,
			FailedAtStep: intPtr(0),
			DurationMs:   r.deps.Clock.NowMs() - startedAt,
			Artifacts:    artifacts,
		}, nil
	}

	stepResults := make([]StepResult, 0, len(def.Steps))
	var failedAtStep *int
	var abort *RunAbort

	// Optional inter-step delay. Default 0, since Orchestra actions and the
	// wait primitives synchronize on hierarchy() state, so a fixed pad
	// is unnecessary for correctness. A script that needs a deliberate
	// pause between steps (pacing a demo recording, yielding to an
	// external process) sets stepDelay explicitly. Applies only to the
	// top-level step loop, not to a nested do/setup list run through
	// RunSteps.
	stepDelay := 0.0
	if def.StepDelay != nil {
		stepDelay = *def.StepDelay
	}

	for i, step := range def.Steps {
		if err := ctx.Err(); err != nil {
			return r.interrupted(def, stepResults, startedAt, artifacts), err
		}

		cmd, cmdOk := r.registry[step.StepCommand()]

		if !cmdOk {
			stepResults = append(stepResults, StepResult{
				StepIndex: i,
				Command:   step.StepCommand(),
				Ok:        false,
				Detail:    strPtr(fmt.Sprintf("Unknown command: %q", step.StepCommand())),
			})
			idx := i
			failedAtStep = &idx
			break
		}

		cmdCtx := r.contextFor(ctx, rs, i)
		if skip, skippable := cmd.(conditionalCommand); skippable && skip.shouldSkip(cmdCtx) {
			continue
		}

		if i > 0 && stepDelay > 0 && step.StepCommand() != "sleep" {
			if err := r.deps.Clock.Sleep(ctx, int64(stepDelay)); err != nil {
				return ScriptResult{}, err
			}
		}

		line := lineForStep(def, i)
		if err := r.gateStep(ctx, rs, r.stepLocation(line), step); err != nil {
			return r.interrupted(def, stepResults, startedAt, artifacts), err
		}

		myIndex := rs.globalStepIndex
		rs.globalStepIndex++

		summary := SummarizeStep(step)
		r.resetTrail()
		r.emitStep(StepEvent{
			Type: StepStarted, StepIndex: myIndex, Command: step.StepCommand(),
			Summary: summary.Text, Tokens: summary.Tokens, Depth: 0, Line: line,
		})

		stepStart := r.deps.Clock.NowMs()
		result := r.execute(cmd, step, cmdCtx)
		durationMs := r.deps.Clock.NowMs() - stepStart

		stepResults = append(stepResults, StepResult{
			StepIndex:  i,
			Command:    step.StepCommand(),
			Ok:         result.Ok,
			Detail:     result.Detail,
			DurationMs: durationMs,
		})
		r.emitStep(StepEvent{
			Type: StepCompleted, StepIndex: myIndex, Command: step.StepCommand(),
			Summary: summary.Text, Tokens: summary.Tokens, Ok: result.Ok, Detail: result.Detail,
			DurationMs: durationMs, Depth: 0, Line: line,
		})

		if rs.gateErr != nil {
			return r.interrupted(def, stepResults, startedAt, artifacts), rs.gateErr
		}

		if !result.Ok {
			idx := i
			failedAtStep = &idx
			abort = r.classifyStepFailure(ctx)
			if abort != nil {
				r.stepLog.Debug("script.step.deviceLost", logging.LogContext{
					"stepIndex": myIndex,
					"rawDetail": detailText(result.Detail),
				})
				neutral := deviceLostDetail(abort)
				stepResults[len(stepResults)-1].Detail = &neutral
			} else {
				r.captureFailure(ctx, StepFailure{
					StepNumber: i + 1, Command: step.StepCommand(), Detail: result.Detail,
				})
			}
			break
		}
	}

	passedSteps := 0
	for _, s := range stepResults {
		if s.Ok {
			passedSteps++
		}
	}

	return ScriptResult{
		ScriptName:   def.Name,
		Ok:           failedAtStep == nil,
		Steps:        stepResults,
		TotalSteps:   len(def.Steps),
		PassedSteps:  passedSteps,
		FailedAtStep: failedAtStep,
		DurationMs:   r.deps.Clock.NowMs() - startedAt,
		Artifacts:    artifacts,
		Abort:        abort,
	}, nil
}

// interrupted is the result of a run that a context end or a StepGate
// error stopped between steps. It carries the steps that ran before the
// stop and is never Ok, so a host that knows why the run stopped can
// report it without losing them.
func (r *ScriptRunner) interrupted(def *script.ScriptDefinition, steps []StepResult, startedAt int64, artifacts ScriptArtifacts) ScriptResult {
	passed := 0
	for _, s := range steps {
		if s.Ok {
			passed++
		}
	}
	return ScriptResult{
		ScriptName:  def.Name,
		Steps:       steps,
		TotalSteps:  len(def.Steps),
		PassedSteps: passed,
		DurationMs:  r.deps.Clock.NowMs() - startedAt,
		Artifacts:   artifacts,
	}
}

// classifyStepFailure runs a bounded liveness probe after a step has
// already failed, so the runner can tell an ordinary step failure
// apart from one caused by the device or agent going unreachable. It
// returns nil when the probe reports the agent healthy, in which case
// the failure stays an ordinary one and the caller's existing
// behavior is unchanged. It is never called on the happy path, so a
// script whose steps all pass never pays for the probe.
func (r *ScriptRunner) classifyStepFailure(ctx context.Context) *RunAbort {
	// A step can fail because its own context was canceled or hit its
	// deadline (a user stop, or a run-level timeout), not because the
	// device went away. Probing liveness on that same context would
	// only ever observe the cancellation itself, misreporting an
	// ordinary cancellation as a device-lost abort, so the failure
	// stays an ordinary one instead of being classified at all.
	if ctx.Err() != nil {
		return nil
	}
	report := r.deps.Orchestra.CheckHealth(ctx)
	if report.Healthy {
		return nil
	}
	return &RunAbort{
		Reason:  RunAbortDeviceLost,
		Code:    report.Code,
		Message: report.Message,
		Remedy:  report.Remedy,
	}
}

// deviceLostDetail builds the failed step's reported detail for a
// device-lost abort from the classified RunAbort rather than the raw
// transport error the step itself returned, so a caller at the tool
// boundary never sees the underlying protocol, route, or port. It
// folds Remedy into the same string because the wire result carries
// one detail field per step, with no separate remedy slot. Joining
// trims Message's own trailing sentence punctuation and spaces first,
// so the fixed ". " separator never produces a doubled period when
// Message already ends a sentence.
func deviceLostDetail(abort *RunAbort) string {
	if abort.Remedy == "" {
		return abort.Message
	}
	message := strings.TrimRight(strings.TrimSpace(abort.Message), ".!?")
	return message + ". " + abort.Remedy
}

// detailText renders a step's raw detail for a debug log line, empty
// when the step reported none.
func detailText(detail *string) string {
	if detail == nil {
		return ""
	}
	return *detail
}

// contextFor builds the CommandContext for stepIndex within rs,
// closing RunSteps and RunPreconditions over ctx so a command that
// invokes nested execution still observes the caller's cancellation.
func (r *ScriptRunner) contextFor(ctx context.Context, rs *runState, stepIndex int) *CommandContext {
	return &CommandContext{
		Context:          ctx,
		Orchestra:        r.deps.Orchestra,
		Clock:            r.deps.Clock,
		Logger:           r.deps.Logger,
		Variables:        rs.variables,
		Secrets:          rs.secrets,
		Captures:         rs.captures,
		NetworkCapture:   r.deps.NetworkCapture,
		StepIndex:        stepIndex,
		Script:           rs.def,
		LaunchNoReset:    r.deps.LaunchNoReset,
		ClearDataOnFresh: r.deps.ClearDataOnFresh,
		AppArtifact:      r.deps.AppArtifact,
		Artifacts:        rs.artifacts,
		FlowLoader:       r.deps.FlowLoader,
		RunSteps: func(steps []script.ScriptStep) CommandResult {
			return r.runNestedSteps(ctx, rs, steps)
		},
		RunPreconditions: func(preconditions []script.Precondition) CommandResult {
			return r.runPreconditions(ctx, rs, preconditions)
		},
	}
}

// execute runs cmd against step and ctx, recovering a handler panic
// into a failed CommandResult with an "Exception: " prefixed detail
// so the run still completes instead of crashing the host process. A
// panic reaching this point is always a bug in the command handler,
// never an expected outcome, since every handler converts a known,
// user-facing failure into an ordinary CommandResult itself.
//
// The recovered value is logged at error severity so the failure stays
// visible by default, but without the goroutine stack: a stack trace
// names local file paths and is noise for anyone not debugging the
// handler itself. The stack is logged separately at debug severity, so
// it is available on demand (raising the CLI's log level) without
// appearing in default output.
func (r *ScriptRunner) execute(cmd CommandDefinition, step script.ScriptStep, ctx *CommandContext) (result CommandResult) {
	defer func() {
		if script.WrittenFormLost(step) {
			result = withheldResult(step, result)
		}
	}()
	defer func() {
		if rec := recover(); rec != nil {
			if canceled, err := cancellationPanic(rec, ctx.Context); canceled {
				result = CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Interrupted: %v", err))}
				return
			}
			panicText := fmt.Sprintf("%v", rec)
			if script.WrittenFormLost(step) {
				panicText = "withheld"
			}
			r.deps.Logger.Error("command handler panicked", nil, logging.LogContext{
				"command": step.StepCommand(),
				"panic":   panicText,
			})
			r.deps.Logger.Debug("command handler panicked", logging.LogContext{
				"command": step.StepCommand(),
				"panic":   panicText,
				"stack":   string(debug.Stack()),
			})
			result = CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Exception: %v", rec))}
		}
	}()
	return cmd.Execute(step, ctx)
}

// cancellationPanic reports whether rec is the context error a command
// handler raised because the run's own context ended, which is how a
// handler unwinds from a wait that the caller interrupted. That is an
// expected stop, not a handler bug, so it is not logged as a panic.
func cancellationPanic(rec any, runCtx context.Context) (bool, error) {
	err, isErr := rec.(error)
	if !isErr || runCtx == nil || runCtx.Err() == nil {
		return false, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true, err
	}
	return false, nil
}

// runNestedSteps executes steps in rs's shared variables and
// captures, failing fast on the first step whose result is not Ok.
// Context cancellation here ends the nested run with a failed
// CommandResult rather than propagating ctx.Err() as a Go error:
// CommandDefinition.Execute has no error return, so a handle, branch,
// or runFlow command that calls CommandContext.RunSteps has no other
// way to observe it.
//
// Every step that actually dispatches (an unknown command bails
// before this point) emits a StepStarted/StepCompleted pair around
// its execution, at the depth this whole nested list runs at, one
// level deeper than the caller, restored once the list finishes or
// fails.
func (r *ScriptRunner) runNestedSteps(ctx context.Context, rs *runState, steps []script.ScriptStep) CommandResult {
	rs.depth++
	defer func() { rs.depth-- }()

	for i, step := range steps {
		if err := ctx.Err(); err != nil {
			return CommandResult{Ok: false, Detail: strPtr(err.Error())}
		}
		cmd, ok := r.registry[step.StepCommand()]
		if !ok {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Unknown command: %q", step.StepCommand()))}
		}

		cmdCtx := r.contextFor(ctx, rs, i)
		if skip, skippable := cmd.(conditionalCommand); skippable && skip.shouldSkip(cmdCtx) {
			continue
		}

		if err := r.gateStep(ctx, rs, "", step); err != nil {
			rs.gateErr = err
			return CommandResult{Ok: false, Detail: strPtr(err.Error())}
		}

		myIndex := rs.globalStepIndex
		rs.globalStepIndex++

		summary := SummarizeStep(step)
		r.emitStep(StepEvent{
			Type: StepStarted, StepIndex: myIndex, Command: step.StepCommand(),
			Summary: summary.Text, Tokens: summary.Tokens, Depth: rs.depth,
		})

		stepStart := r.deps.Clock.NowMs()
		result := r.execute(cmd, step, cmdCtx)
		durationMs := r.deps.Clock.NowMs() - stepStart

		r.emitStep(StepEvent{
			Type: StepCompleted, StepIndex: myIndex, Command: step.StepCommand(),
			Summary: summary.Text, Tokens: summary.Tokens, Ok: result.Ok, Detail: result.Detail,
			DurationMs: durationMs, Depth: rs.depth,
		})

		if !result.Ok {
			return result
		}
	}
	return CommandResult{Ok: true, Detail: strPtr("completed")}
}

// lineForStep reports def's declared source line for step i, or nil
// when the script carries no line mapping for it, either because
// def.StepLines is shorter than its Steps (an older parse, or a
// programmatically built definition) or because the parser recorded
// 0, its sentinel for "unknown". Source lines are 1-based, so a
// recorded 0 can never be a real line number and safely doubles as
// "absent".
func lineForStep(def *script.ScriptDefinition, i int) *int {
	if i >= len(def.StepLines) {
		return nil
	}
	line := def.StepLines[i]
	if line == 0 {
		return nil
	}
	return &line
}

// runPreconditions evaluates preconditions in order, before the steps
// they gate run. It returns a failed CommandResult on the first
// precondition that does not hold after its setup ran; the caller
// (Run, or a future handle/branch/runFlow command through
// CommandContext.RunPreconditions) treats that exactly like a failing
// step. It never panics for an ordinary script-authoring mistake (a
// missing setup file, a check that fails to hold).
func (r *ScriptRunner) runPreconditions(ctx context.Context, rs *runState, preconditions []script.Precondition) CommandResult {
	if len(preconditions) == 0 {
		return CommandResult{Ok: true, Detail: strPtr("no preconditions")}
	}

	for _, precondition := range preconditions {
		if err := ctx.Err(); err != nil {
			return CommandResult{Ok: false, Detail: strPtr(err.Error())}
		}

		policy := resolveEffectivePolicy(r.deps.PreconditionsOverride, precondition.Policy)
		if policy == "skip" {
			continue
		}

		if policy == "always" {
			result := r.runSetupDeduped(ctx, rs, precondition)
			if !result.Ok {
				return result
			}

			// Setup ran, but a script author can misconfigure it (or
			// it can no-op against current device state) without
			// raising an error, so the check is re-run to confirm it
			// actually holds before steps proceed.
			holds, err := r.evaluateCheck(ctx, precondition.Check)
			if err != nil {
				return CommandResult{Ok: false, Detail: strPtr(err.Error())}
			}
			if !holds {
				return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
					"Precondition not met after setup: %s", describeCheck(precondition.Check)))}
			}
			continue
		}

		// "ensure": the check is the guard. Skip setup entirely when
		// it already holds.
		holds, err := r.evaluateCheck(ctx, precondition.Check)
		if err != nil {
			return CommandResult{Ok: false, Detail: strPtr(err.Error())}
		}
		if holds {
			continue
		}

		// The check failed, so run setup now, deliberately bypassing
		// the dedup set: an earlier "ensure" precondition may have
		// run this exact setup already, but device state can regress
		// between then and now, and only re-running setup recovers
		// it. Dedup stays reserved for "always" / "force" setups,
		// which run unconditionally and so only need to run once.
		setupResult := r.runSetup(ctx, rs, precondition)
		if !setupResult.Ok {
			return setupResult
		}

		holds, err = r.evaluateCheck(ctx, precondition.Check)
		if err != nil {
			return CommandResult{Ok: false, Detail: strPtr(err.Error())}
		}
		if !holds {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
				"Precondition not met after setup: %s", describeCheck(precondition.Check)))}
		}
	}

	return CommandResult{Ok: true, Detail: strPtr("preconditions satisfied")}
}

// runRequiredFlows executes every flow def.Requires names, in order,
// before def's own preconditions and steps run: each required flow's
// preconditions and steps run in this run's shared variables and
// captures, exactly like a runFlow step's target. A required flow
// already completed earlier in this ScriptRunner's lifetime (see the
// completedRequires field) is skipped; a required flow already on the
// call stack, reachable only when the FlowLoader configured for this
// runner itself re-enters ScriptRunner.Run for a nested flow that
// requires an ancestor, is a circular dependency and fails the run
// immediately, running nothing further. Returns a nil ScriptResult
// and a nil error when every required flow completed successfully. A
// non-nil error means the flow file itself could not be loaded (not
// found, or failed to parse), which aborts Run outright as a Go error
// instead of producing a failed ScriptResult: a broken required-flow
// reference is a setup problem the caller must fix, not a reportable
// test outcome.
func (r *ScriptRunner) runRequiredFlows(
	ctx context.Context, rs *runState, requires []string, startedAt int64, artifacts ScriptArtifacts,
) (*ScriptResult, error) {
	requiresFail := func(detail string) *ScriptResult {
		return &ScriptResult{
			ScriptName: rs.def.Name,
			Ok:         false,
			Steps: []StepResult{{
				StepIndex: -1,
				Command:   "requires",
				Ok:        false,
				Detail:    strPtr(detail),
			}},
			TotalSteps:   len(rs.def.Steps),
			PassedSteps:  0,
			FailedAtStep: intPtr(0),
			DurationMs:   r.deps.Clock.NowMs() - startedAt,
			Artifacts:    artifacts,
		}
	}

	for _, reqPath := range requires {
		absPath := reqPath
		if abs, err := filepath.Abs(reqPath); err == nil {
			absPath = abs
		}

		r.requiresMu.Lock()
		_, done := r.completedRequires[absPath]
		_, inStack := r.callStack[absPath]
		if !done && !inStack {
			r.callStack[absPath] = struct{}{}
		}
		r.requiresMu.Unlock()

		if done {
			continue
		}
		if inStack {
			return requiresFail(fmt.Sprintf("Circular dependency detected: %s", reqPath)), nil
		}

		failure, err := r.runOneRequiredFlow(ctx, rs, reqPath, absPath, requiresFail)

		r.requiresMu.Lock()
		delete(r.callStack, absPath)
		if failure == nil && err == nil {
			r.completedRequires[absPath] = struct{}{}
		}
		r.requiresMu.Unlock()

		if err != nil {
			return nil, err
		}
		if failure != nil {
			return failure, nil
		}
	}
	return nil, nil
}

// runOneRequiredFlow loads reqPath (already resolved to absPath) and
// runs its preconditions, then its steps, in rs's shared context. It
// returns a nil ScriptResult and a nil error on success, the
// ScriptResult requiresFail already shaped for the caller to return
// from Run when the flow loaded but its preconditions or steps
// failed, or a non-nil error when the flow itself could not be
// loaded. The caller returns that error from Run directly instead of
// wrapping it in a ScriptResult.
func (r *ScriptRunner) runOneRequiredFlow(
	ctx context.Context, rs *runState, reqPath, absPath string, requiresFail func(string) *ScriptResult,
) (*ScriptResult, error) {
	if r.deps.FlowLoader == nil {
		return requiresFail(fmt.Sprintf("no flow loader configured to resolve required flow: %s", reqPath)), nil
	}
	parentEnv := environmentFromVariables(rs.variables)
	reqDef, err := r.deps.FlowLoader.LoadFlow(absPath, parentEnv)
	if err != nil {
		return nil, fmt.Errorf("required flow %q: %w", reqPath, err)
	}

	preconditionResult := r.runPreconditions(ctx, rs, reqDef.Preconditions)
	if !preconditionResult.Ok {
		return requiresFail(fmt.Sprintf(
			"Required flow %q precondition failed: %s", reqPath, detailOrEmpty(preconditionResult.Detail))), nil
	}

	stepsResult := r.runNestedSteps(ctx, rs, reqDef.Steps)
	if !stepsResult.Ok {
		return requiresFail(fmt.Sprintf("Required flow %q failed: %s", reqPath, detailOrEmpty(stepsResult.Detail))), nil
	}

	return nil, nil
}

// resolveEffectivePolicy resolves a precondition's effective
// enforcement for this run. The run-level override always wins: skip
// suppresses every precondition regardless of its own policy
// (including one set to "always"), and force runs every setup
// unconditionally (including one whose own policy is "skip"). With no
// override, the precondition's own policy applies, defaulting to
// "ensure".
func resolveEffectivePolicy(override PreconditionsOverride, own *string) string {
	switch override {
	case PreconditionsSkip:
		return "skip"
	case PreconditionsForce:
		return "always"
	}
	if own != nil {
		return *own
	}
	return "ensure"
}

// runSetup runs setup unconditionally, resolving a file-referenced
// setup into steps first through FlowLoader. It returns a clean
// failure result (never a panic) when the referenced file cannot be
// loaded.
func (r *ScriptRunner) runSetup(ctx context.Context, rs *runState, precondition script.Precondition) CommandResult {
	steps, detail := r.resolveSteps(precondition.Setup, environmentFromVariables(rs.variables))
	if detail != nil {
		return CommandResult{Ok: false, Detail: detail}
	}
	return r.runNestedSteps(ctx, rs, steps)
}

// runSetupDeduped runs setup at most once per run, keyed by
// setupDedupKey. Used only for "always" / run-level "force" setups,
// which run unconditionally and therefore need the dedup guard so a
// setup reached from two preconditions does not repeat.
func (r *ScriptRunner) runSetupDeduped(ctx context.Context, rs *runState, precondition script.Precondition) CommandResult {
	key := setupDedupKey(precondition.Setup)
	if _, done := rs.completedPreconditionSetups[key]; done {
		return CommandResult{Ok: true, Detail: strPtr("setup already completed this run")}
	}
	result := r.runSetup(ctx, rs, precondition)
	if result.Ok {
		rs.completedPreconditionSetups[key] = struct{}{}
	}
	return result
}

// resolveSteps resolves a setup/do/default field into a concrete step
// list, loading it through FlowLoader when it names a file and
// interpolating its `${var}` placeholders against env.
func (r *ScriptRunner) resolveSteps(sof script.StepsOrFile, env map[string]string) ([]script.ScriptStep, *string) {
	if sof.File != nil {
		if r.deps.FlowLoader == nil {
			return nil, strPtr(fmt.Sprintf("no flow loader configured to resolve setup file: %s", *sof.File))
		}
		flow, err := r.deps.FlowLoader.LoadFlow(*sof.File, env)
		if err != nil {
			return nil, strPtr(fmt.Sprintf("precondition setup file %q: %s", *sof.File, err))
		}
		return flow.Steps, nil
	}
	return sof.Steps, nil
}

// setupDedupKey builds a dedup key for setup. StepsOrFile's own
// MarshalJSON renders a file reference as a quoted JSON string and an
// inline step list as a JSON array, two shapes that can never collide,
// so two preconditions that author the identical inline setup, or
// name the same file, coalesce onto the same key with no extra
// prefixing.
func setupDedupKey(sof script.StepsOrFile) string {
	data, err := json.Marshal(sof)
	if err != nil {
		return fmt.Sprintf("unmarshalable:%p", &sof)
	}
	return string(data)
}

// toScriptSelector expands the `visible` / `notVisible` shorthand: a
// bare string matches both text and label, because Flutter exposes
// visible text as label.
func toScriptSelector(v script.StringOrSelector) script.ScriptSelector {
	if v.Str != nil {
		s := *v.Str
		return script.ScriptSelector{Text: &s, Label: &s}
	}
	if v.Selector != nil {
		return *v.Selector
	}
	return script.ScriptSelector{}
}

// compileScriptSelector converts a script-level selector into the
// Selector shape Orchestra understands. Script selectors are
// intentionally simpler than MCP selectors (no regex), so this
// conversion is a direct field mapping.
func compileScriptSelector(sel script.ScriptSelector) orchestra.Selector {
	out := orchestra.Selector{Role: sel.Role, Nth: sel.Nth}
	if sel.ID != nil {
		p := orchestra.LiteralPattern(*sel.ID)
		out.ID = &p
	}
	if sel.Text != nil {
		p := orchestra.LiteralPattern(*sel.Text)
		out.Text = &p
	}
	if sel.Label != nil {
		p := orchestra.LiteralPattern(*sel.Label)
		out.Label = &p
	}
	if sel.Hint != nil {
		p := orchestra.LiteralPattern(*sel.Hint)
		out.Hint = &p
	}
	return out
}

// evaluateCheck reports whether check currently holds against the
// device's accessibility tree.
func (r *ScriptRunner) evaluateCheck(ctx context.Context, check script.PreconditionCheck) (bool, error) {
	if check.Visible != nil {
		sel := compileScriptSelector(toScriptSelector(*check.Visible))
		found, err := r.deps.Orchestra.FindOne(ctx, sel)
		if err != nil {
			return false, err
		}
		return found != nil, nil
	}
	if check.NotVisible != nil {
		sel := compileScriptSelector(toScriptSelector(*check.NotVisible))
		found, err := r.deps.Orchestra.FindOne(ctx, sel)
		if err != nil {
			return false, err
		}
		return found == nil, nil
	}
	// ParseScript rejects a script whose check sets neither or both
	// of visible/notVisible, so a Precondition built through it never
	// reaches this branch. Fail closed rather than vacuously pass, in
	// case a caller constructs one outside the parser.
	return false, nil
}

func describeCheck(check script.PreconditionCheck) string {
	if check.Visible != nil {
		return fmt.Sprintf("expected visible %s", describeSelectorValue(*check.Visible))
	}
	if check.NotVisible != nil {
		return fmt.Sprintf("expected not visible %s", describeSelectorValue(*check.NotVisible))
	}
	return "no check configured"
}

func describeSelectorValue(v script.StringOrSelector) string {
	if v.Str != nil {
		return fmt.Sprintf("%q", *v.Str)
	}
	data, _ := json.Marshal(v.Selector)
	return string(data)
}

// newRunArtifacts returns the injected artifacts collector, or a
// fresh in-memory one when none was injected, so Run always has a
// non-nil ScriptArtifacts to hand to every command even when the
// caller configured none.
func (r *ScriptRunner) newRunArtifacts() ScriptArtifacts {
	if r.deps.Artifacts != nil {
		return r.deps.Artifacts
	}
	return newInMemoryArtifacts()
}

// inMemoryArtifacts is the default ScriptArtifacts: an in-memory
// screenshot collector with no external dependency.
type inMemoryArtifacts struct {
	screenshots []Screenshot
}

func newInMemoryArtifacts() *inMemoryArtifacts {
	return &inMemoryArtifacts{}
}

func (a *inMemoryArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	a.screenshots = append(a.screenshots, Screenshot{Label: label, Data: data, MimeType: mimeType})
}

func (a *inMemoryArtifacts) Screenshots() []Screenshot {
	return a.screenshots
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
