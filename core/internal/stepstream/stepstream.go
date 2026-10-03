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
	AppendStep(runfolder.StepLine) (runfolder.StepLine, bool, error)
}

// Option tunes NewSink.
type Option func(*sinkConfig)

type sinkConfig struct {
	publish func(runfolder.StepLine)
}

// WithPublisher makes the sink hand every line that was appended to
// publish, stamped with its schema version and sequence number. publish
// must not block, and a nil publish changes nothing. Lines are published
// in sequence order even when events arrive from several goroutines. A
// line that was not appended, because the append failed or the file is
// full, is not published, because it has no sequence number.
func WithPublisher(publish func(runfolder.StepLine)) Option {
	return func(c *sinkConfig) { c.publish = publish }
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
func NewSink(appender Appender, logger logging.Logger, opts ...Option) func(runner.StepEvent) {
	var cfg sinkConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	var warn sync.Once
	var mu sync.Mutex
	return func(event runner.StepEvent) {
		mu.Lock()
		defer mu.Unlock()
		stamped, appended, err := appender.AppendStep(lineFor(event))
		if err != nil {
			warn.Do(func() {
				logger.Warn("run.step_line_failed", logging.LogContext{"error": err.Error()})
			})
			return
		}
		if appended && cfg.publish != nil {
			cfg.publish(stamped)
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
