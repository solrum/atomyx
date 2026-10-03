package stepstream

import (
	"errors"
	"testing"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
)

type recordingAppender struct {
	lines []runfolder.StepLine
	err   error
}

func (r *recordingAppender) AppendStep(line runfolder.StepLine) (runfolder.StepLine, error) {
	r.lines = append(r.lines, line)
	return line, r.err
}

type countingLogger struct {
	logging.Logger
	warns int
}

func (c *countingLogger) Warn(string, logging.LogContext) { c.warns++ }

func TestSinkMapsEachPhaseOfAStep(t *testing.T) {
	appender := &recordingAppender{}
	sink := NewSink(appender, &countingLogger{})
	detail := "Element exists"

	sink(runner.StepEvent{Type: runner.StepStarted, StepIndex: 4, Command: "assertExists", Summary: "assertExists: x", Ok: true, DurationMs: 9})
	sink(runner.StepEvent{Type: runner.StepCompleted, StepIndex: 4, Command: "assertExists", Ok: true, Detail: &detail, DurationMs: 12})

	started, completed := appender.lines[0], appender.lines[1]
	if started.Phase != runfolder.StepPhaseStarted || started.StepIndex != 4 || started.Command != "assertExists" {
		t.Errorf("started line = %+v", started)
	}
	if started.Ok || started.DurationMs != 0 || started.Detail != nil {
		t.Errorf("a started line must carry no outcome: %+v", started)
	}
	if completed.Phase != runfolder.StepPhaseCompleted || !completed.Ok || completed.DurationMs != 12 || completed.Detail == nil || *completed.Detail != detail {
		t.Errorf("completed line = %+v", completed)
	}
}

func TestSinkLogsAFailedAppendOnceAndKeepsGoing(t *testing.T) {
	appender := &recordingAppender{err: errors.New("disk full")}
	logger := &countingLogger{}
	sink := NewSink(appender, logger)

	for i := 0; i < 3; i++ {
		sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})
	}

	if len(appender.lines) != 3 || logger.warns != 1 {
		t.Fatalf("appended %d lines and logged %d warnings, want 3 and 1", len(appender.lines), logger.warns)
	}
}
