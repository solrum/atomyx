package runner

import (
	"context"
	"fmt"

	"dev.atomyx/internal/logging"
)

// StepEventType discriminates a StepEvent's variant.
type StepEventType string

const (
	// StepStarted fires immediately before a step executes.
	StepStarted StepEventType = "stepStarted"
	// StepCompleted fires immediately after a step executes, whether
	// it succeeded, failed, or raised an exception the runner recovered.
	StepCompleted StepEventType = "stepCompleted"
)

// StepEvent is a per-step progress notification the runner emits
// through ScriptRunnerDeps.OnStep. It flattens two logically distinct
// shapes into one struct: Ok, Detail, and DurationMs are meaningful
// only when Type is StepCompleted, and are the zero value when Type
// is StepStarted.
type StepEvent struct {
	// Type discriminates StepStarted from StepCompleted.
	Type StepEventType
	// StepIndex is a monotonic counter spanning the whole run: every
	// required flow's steps, every precondition setup's steps, and the
	// main script's own steps each consume the next index in the order
	// they execute. It is unrelated to StepResult.StepIndex, which
	// counts only positions within the main script's own step list.
	StepIndex int
	// Command is the step's command name.
	Command string
	// Summary is the step's one-line plain-text rendering, from
	// SummarizeStep.
	Summary string
	// Tokens is the step's colored token rendering, from SummarizeStep.
	Tokens []StepToken
	// Depth is the nesting level this step ran at: 0 for a step in the
	// main script's own step list, 1 for a step inside a required
	// flow, a precondition's setup, or a handle/branch/runFlow's
	// nested step list run through CommandContext.RunSteps, and one
	// higher for each further level of nesting reached the same way.
	Depth int
	// Line is the step's 1-based source line in the script's own YAML
	// text, or nil for a step from a nested step list (a required
	// flow, a precondition setup, or a handle/branch/runFlow target),
	// which carries no line mapping into the running script's source.
	Line *int
	// Ok reports whether the step succeeded. Meaningful only when Type
	// is StepCompleted.
	Ok bool
	// Detail is the step's result detail. Meaningful only when Type is
	// StepCompleted; nil when the step reported none.
	Detail *string
	// DurationMs is how long the step took to execute. Meaningful only
	// when Type is StepCompleted.
	DurationMs int64
	// Attempt is the 1-based attempt number of a retried suite entry
	// that emitted the event. It is zero for a first attempt and for any
	// run that is not a suite entry.
	Attempt int
}

// emitStep logs event's outcome when it is a StepCompleted event, then
// invokes the configured OnStep hook with event, doing nothing further
// when no hook is configured. A panic from the hook is recovered so a
// misbehaving consumer cannot crash the run. The recovered value is
// logged rather than discarded, because a panicking hook is a bug in
// the host's own step-reporting code and must stay visible even though
// the run itself continues.
func (r *ScriptRunner) emitStep(event StepEvent) {
	r.recordTrail(event)
	if event.Type == StepCompleted {
		r.stepLog.Info("script.step", logging.LogContext{
			"command":    event.Command,
			"stepIndex":  event.StepIndex,
			"depth":      event.Depth,
			"ok":         event.Ok,
			"durationMs": event.DurationMs,
		})
	}

	hook := r.deps.OnStep
	if hook == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.stepLog.Error("step event hook panicked", nil, logging.LogContext{
				"panic": fmt.Sprintf("%v", rec),
			})
		}
	}()
	hook(event)
}

func (r *ScriptRunner) resetTrail() {
	r.trailMu.Lock()
	defer r.trailMu.Unlock()
	r.trail = r.trail[:0]
}

func (r *ScriptRunner) recordTrail(event StepEvent) {
	r.trailMu.Lock()
	defer r.trailMu.Unlock()
	r.trail = append(r.trail, event)
}

// captureFailure hands failure, completed with the events of the failed
// step, to the configured FailureCapture. A capture that panics is
// logged and discarded so evidence gathering can never change the run
// outcome.
func (r *ScriptRunner) captureFailure(ctx context.Context, failure StepFailure, mask func(string) string) {
	if r.deps.FailureCapture == nil || ctx.Err() != nil {
		return
	}
	r.trailMu.Lock()
	failure.Events = make([]StepEvent, len(r.trail))
	for i, event := range r.trail {
		failure.Events[i] = event.Masked(mask)
	}
	r.trailMu.Unlock()
	failure.Detail = MaskedText(failure.Detail, mask)

	defer func() {
		if rec := recover(); rec != nil {
			r.stepLog.Error("failure capture panicked", nil, logging.LogContext{
				"panic": fmt.Sprintf("%v", rec),
			})
		}
	}()
	r.deps.FailureCapture.CaptureFailure(ctx, failure)
}
