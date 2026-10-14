package stepstream

import (
	"errors"
	"runtime"
	"sync"
	"testing"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
)

type recordingAppender struct {
	lines []runfolder.StepLine
	err   error
}

func (r *recordingAppender) AppendStep(line runfolder.StepLine) (runfolder.StepLine, bool, error) {
	r.lines = append(r.lines, line)
	return line, r.err == nil, r.err
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

type stampingAppender struct {
	mu   sync.Mutex
	next int64
}

func (a *stampingAppender) AppendStep(line runfolder.StepLine) (runfolder.StepLine, bool, error) {
	a.mu.Lock()
	a.next++
	line.Seq = a.next
	a.mu.Unlock()
	runtime.Gosched()
	return line, true, nil
}

func TestSinkPublishesEveryStampedLineInOrder(t *testing.T) {
	var published []runfolder.StepLine
	sink := NewSink(&stampingAppender{}, &countingLogger{}, WithPublisher(func(l runfolder.StepLine) {
		published = append(published, l)
	}))

	sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})
	sink(runner.StepEvent{Type: runner.StepCompleted, Command: "tap", Ok: true})

	if len(published) != 2 || published[0].Seq != 1 || published[1].Seq != 2 || published[1].Phase != runfolder.StepPhaseCompleted {
		t.Fatalf("published = %+v, want two stamped lines in order", published)
	}
}

func TestSinkDoesNotPublishALineThatFailedToAppend(t *testing.T) {
	published := 0
	sink := NewSink(&recordingAppender{err: errors.New("disk full")}, &countingLogger{}, WithPublisher(func(runfolder.StepLine) {
		published++
	}))

	sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})

	if published != 0 {
		t.Fatalf("published %d lines, want none for a failed append", published)
	}
}

type droppingAppender struct{}

func (droppingAppender) AppendStep(line runfolder.StepLine) (runfolder.StepLine, bool, error) {
	return line, false, nil
}

func TestSinkDoesNotPublishALineTheWriterDropped(t *testing.T) {
	published := 0
	sink := NewSink(droppingAppender{}, &countingLogger{}, WithPublisher(func(runfolder.StepLine) {
		published++
	}))

	sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})

	if published != 0 {
		t.Fatalf("published %d lines, want none for a dropped line", published)
	}
}

func TestSinkPublishesInSequenceOrderUnderConcurrentEvents(t *testing.T) {
	var published []runfolder.StepLine
	sink := NewSink(&stampingAppender{}, &countingLogger{}, WithPublisher(func(l runfolder.StepLine) {
		published = append(published, l)
	}))

	const goroutines, perGoroutine = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})
			}
		}()
	}
	wg.Wait()

	if len(published) != goroutines*perGoroutine {
		t.Fatalf("published %d lines, want %d", len(published), goroutines*perGoroutine)
	}
	for i, line := range published {
		if line.Seq != int64(i+1) {
			t.Fatalf("published line %d has seq %d, want %d", i, line.Seq, i+1)
		}
	}
}

func TestSinkCarriesTheAttemptOfARetriedEntry(t *testing.T) {
	appender := &recordingAppender{}
	sink := NewSink(appender, &countingLogger{})

	sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap"})
	sink(runner.StepEvent{Type: runner.StepStarted, Command: "tap", Attempt: 3})

	if appender.lines[0].Attempt != 0 || appender.lines[1].Attempt != 3 {
		t.Errorf("attempts = %d, %d, want 0, 3", appender.lines[0].Attempt, appender.lines[1].Attempt)
	}
}

func TestSinkMasksTheDetailOfTheAppendedAndThePublishedLine(t *testing.T) {
	appender := &recordingAppender{}
	var published []runfolder.StepLine
	sink := NewSink(appender, &countingLogger{},
		WithMask(func(s string) string { return "masked:" + s }),
		WithPublisher(func(l runfolder.StepLine) { published = append(published, l) }))
	detail := "raw"

	sink(runner.StepEvent{Type: runner.StepCompleted, Command: "tap", Detail: &detail})

	if got := *appender.lines[0].Detail; got != "masked:raw" {
		t.Errorf("appended detail = %q", got)
	}
	if got := *published[0].Detail; got != "masked:raw" {
		t.Errorf("published detail = %q", got)
	}
	if detail != "raw" {
		t.Errorf("the event detail was rewritten: %q", detail)
	}
}
