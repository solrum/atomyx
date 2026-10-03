// Package stepstream turns the step events of a script run into the
// lines of a run folder's steps.jsonl.
package stepstream

import (
	"sync"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
)

// Appender is the part of a run folder writer a Sink needs.
type Appender interface {
	AppendStep(runfolder.StepLine) (runfolder.StepLine, error)
}

// NewSink returns a runner.ScriptRunnerDeps.OnStep hook that appends one
// step line per event to appender, so a run produces a started line and
// a completed line for every step in the order the runner emitted them.
//
// A line carries the step's command, its run-wide index, and for a
// completed event the outcome, duration and detail. The detail is the
// text the runner reports for the step, which never includes typed text.
// A failed append is logged once and later failures are dropped, because
// the step stream is observability and must never change a run's result.
func NewSink(appender Appender, logger logging.Logger) func(runner.StepEvent) {
	var warn sync.Once
	return func(event runner.StepEvent) {
		if _, err := appender.AppendStep(lineFor(event)); err != nil {
			warn.Do(func() {
				logger.Warn("run.step_line_failed", logging.LogContext{"error": err.Error()})
			})
		}
	}
}

func lineFor(event runner.StepEvent) runfolder.StepLine {
	line := runfolder.StepLine{
		StepIndex: event.StepIndex,
		Command:   event.Command,
		Phase:     runfolder.StepPhaseStarted,
	}
	if event.Type == runner.StepCompleted {
		line.Phase = runfolder.StepPhaseCompleted
		line.Ok = event.Ok
		line.DurationMs = event.DurationMs
		line.Detail = event.Detail
	}
	return line
}
