package runner

import (
	"context"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// Screenshot is one entry a ScriptArtifacts collector holds.
// MimeType names the encoding of Data (for example "image/png" or
// "image/jpeg") so a consumer that persists it knows which file
// extension and content to write.
type Screenshot struct {
	Label    string
	Data     []byte
	MimeType string
}

// ScriptArtifacts collects screenshots and other evidence captured
// during a script run. The runner constructs one per Run call (or
// uses the one injected via ScriptRunnerDeps.Artifacts) and hands it
// to every command through CommandContext; the host reads the
// collected artifacts back from ScriptResult once the run completes.
type ScriptArtifacts interface {
	// AddScreenshot stores data under label for later retrieval.
	// mimeType names the encoding of data (for example "image/png" or
	// "image/jpeg").
	AddScreenshot(label string, data []byte, mimeType string)
	// Screenshots returns every screenshot collected so far, in the
	// order they were added.
	Screenshots() []Screenshot
}

// NetworkCapture observes HTTP traffic during a script run. It is
// read-only by design: the script engine observes API responses but
// never modifies them.
//
// Reset is a required method here because Go interfaces have no
// optional methods: every implementation must supply one. An adapter
// with no accumulated state to discard implements it as a no-op.
type NetworkCapture interface {
	// Start begins capturing traffic.
	Start(ctx context.Context) error
	// Stop stops capturing and releases resources.
	Stop(ctx context.Context) error
	// WaitForRequest waits for a request matching pattern ("METHOD
	// /path", "*" wildcard supported) to appear, up to timeoutMs
	// milliseconds. It returns an error on timeout.
	WaitForRequest(ctx context.Context, pattern string, timeoutMs int) (CapturedRequest, error)
	// All returns every request captured so far.
	All() []CapturedRequest
	// Reset discards requests captured so far without tearing down
	// the underlying session (proxy connection, file watcher), so a
	// caller that reuses one adapter across independent runs can
	// start each run with an empty buffer.
	Reset()
}

// noCaptureConfigured reports whether nc represents "no network
// capture is configured" for a script's `proxy: required` check: nc
// itself is nil, or it is the no-op adapter a composition root wires
// by default when no proxy is set up. A host is expected to always
// pass a non-nil NetworkCapture, even that default no-op, rather than
// leaving the dependency undefined, because checking only for nil
// would let a `proxy: required` script slip past this guard on a
// host that wires the no-op adapter instead.
func noCaptureConfigured(nc NetworkCapture) bool {
	if nc == nil {
		return true
	}
	marker, ok := nc.(interface{ IsNoCaptureConfigured() bool })
	return ok && marker.IsNoCaptureConfigured()
}

// FlowLoader resolves a step list's file reference, which may be a precondition's
// `setup`, a `handle`/`branch` `do`, or a `runFlow` target, into a
// parsed ScriptDefinition, so command handlers and the runner never
// need to touch the filesystem or a parser directly.
//
// env is the external environment the loaded flow's `${var}`
// placeholders resolve against: the run's current variables rendered
// to strings, merged under any step-level override (`runFlow`'s own
// `env` field wins over the parent variable of the same name). A
// loader implementation passes env straight through to
// script.ParseScript.
type FlowLoader interface {
	LoadFlow(path string, env map[string]string) (*script.ScriptDefinition, error)
}

// CommandResult is the outcome of executing a single script command.
type CommandResult struct {
	// Ok reports whether the command succeeded.
	Ok bool
	// Detail is a human-readable explanation: the failure reason
	// when Ok is false, or informational context when Ok is true.
	// Nil when the command has nothing to add.
	Detail *string
	// Code is the diag code of the failure when Ok is false and the failure
	// carries one. Empty otherwise.
	Code string
}

// CommandContext is passed to every command during script execution.
type CommandContext struct {
	// Context is the request-scoped context.Context for the run this
	// step belongs to, canceled when the caller of ScriptRunner.Run
	// cancels its own context. Every command that calls an Orchestra,
	// Clock, or NetworkCapture method passes this through, since none
	// of those ports can be called without one.
	Context context.Context
	// Orchestra is bound to the current device.
	Orchestra *orchestra.Orchestra
	// Clock is the time source for timing and sleep.
	Clock orchestra.Clock
	// Logger is for debug output.
	Logger logging.Logger
	// Variables holds runtime variables: seeded from the script's env
	// block, then extended at runtime (for example by an `extract`
	// command reading an API response). NewVariablesLookup adapts
	// this map for expression syntax such as assertExpr.
	Variables map[string]ScriptValue
	// Secrets names the variables in Variables that hold a secret. A
	// command that writes a variable records its secrecy through
	// markSecret.
	Secrets map[string]bool
	// SecretSink receives the value of every variable markSecret tags.
	// Nil collects nothing.
	SecretSink SecretSink
	// Captures holds captured API responses, keyed by the variable
	// name a `capture ... as: varName` step bound them to.
	Captures map[string]CapturedRequest
	// NetworkCapture is the capture adapter for this run, or nil when
	// no proxy is configured.
	NetworkCapture NetworkCapture
	// StepIndex is the zero-based index of the current step within
	// whichever step list is currently executing: the top-level
	// script, or a nested `do`/`setup` list run through RunSteps.
	StepIndex int
	// Script is the full definition being run, for appId/name access.
	Script *script.ScriptDefinition
	// LaunchNoReset is the `noReset` value a `launchApp` command
	// passes to Orchestra.LaunchApp when the step itself carries no
	// override. Nil is the runner's own default: a plain launch.
	LaunchNoReset *bool
	// ClearDataOnFresh, when true, tells a `launchApp` command to
	// clear app data immediately before a fresh (non-noReset) launch.
	ClearDataOnFresh bool
	// AppArtifact is the installable artifact path for the script's
	// app, when the host's config declares one. Nil when it declares
	// none.
	AppArtifact *string
	// Random supplies the values an inputRandom step generates.
	Random RandomSource
	// Artifacts collects screenshots and other evidence for this run.
	Artifacts ScriptArtifacts
	// RunSteps executes a nested step list, which may be a `handle`/`branch` `do`
	// block or a precondition's `setup`, in this run's shared
	// variables and captures, failing fast on the first step that is
	// not Ok. Provided by the runner; commands must not implement
	// this themselves.
	RunSteps func(steps []script.ScriptStep) CommandResult
	// RunPreconditions evaluates a flow's preconditions before its
	// steps run: it checks each `check`, runs `setup` when unmet, and
	// re-checks afterward. Dedup and the run-level skip-or-force
	// override are owned by the runner and shared across the whole
	// run, so a `setup` referenced from more than one flow still runs
	// at most once. Provided by the runner; commands must not
	// implement this themselves.
	RunPreconditions func(preconditions []script.Precondition) CommandResult
	// FlowLoader resolves a `handle`/`branch` `do` block or a
	// `runFlow` target that names a file into a parsed
	// ScriptDefinition. Nil when the host has not configured
	// file-based flow resolution, in which case a command that needs
	// to load a file reports a failed CommandResult rather than
	// panicking.
	FlowLoader FlowLoader
}

// CommandDefinition is one script command: a name plus the logic that
// executes a step carrying that name against the current device.
//
// Execute never returns a Go error. A handler that reaches a known,
// expected failure (an app that is not installed, a field that never
// takes input focus, a selector that never matches within a timeout)
// converts it into a failed CommandResult itself, the same way it
// reports success. Only a genuinely unexpected failure, one the
// handler has no classification for, panics with the error instead;
// the runner recovers that panic into a failed CommandResult carrying
// an "Exception: " prefixed detail, so a real bug in a handler still
// surfaces as an ordinary failed step instead of crashing the run.
type CommandDefinition interface {
	// Command is the command name; it must match the value
	// ScriptStep.StepCommand() returns for the steps this command
	// handles.
	Command() string
	// Execute runs step, already known by the runner's dispatch on
	// Command() to be the concrete step type this command expects,
	// against ctx.
	Execute(step script.ScriptStep, ctx *CommandContext) CommandResult
}

// conditionalCommand is an opt-in extension to CommandDefinition for a
// command whose step must sometimes be completely invisible to the
// run: no StepStarted or StepCompleted event, no trace log line, and
// no entry in the run's step results, as if the step were never
// present in the script. A command implements this only when it has
// such a condition; the runner treats any command that does not
// implement it as always executing.
type conditionalCommand interface {
	// shouldSkip reports whether the upcoming step must be omitted
	// entirely rather than executed. The runner calls this before
	// emitting StepStarted, so a true result means Execute never runs
	// for this step.
	shouldSkip(step script.ScriptStep, ctx *CommandContext) bool
}

// markSecret records whether the variable name now holds a secret. A
// variable that was secret before the write stays secret whatever the new
// value is, because an external variable such as a login token is secret
// by origin and a value captured into it is as sensitive as the one it
// replaced. Only the value of a non-boolean write reaches the sink, since
// a boolean would mask every "true" or "false" word in the run's output.
func (c *CommandContext) markSecret(name string, secret bool) {
	secret = secret || c.Secrets[name]
	if secret && c.SecretSink != nil && c.Variables[name].Kind() != expression.KindBool {
		c.SecretSink.Add(scriptValueToEnvString(c.Variables[name]))
	}
	if secret && c.Secrets != nil {
		c.Secrets[name] = true
	}
}

// artifactLabel returns label with every secret value of the run replaced
// by the mask. A label names a file in the run folder and a line in the
// report, so a value interpolated into it must not reach either.
func (c *CommandContext) artifactLabel(label string) string {
	return snapshotVariables(c.Variables, c.Secrets).Redactor().String(label)
}
