package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	// recordingRotateInterval is how long one segment runs. It stays below
	// the 180 second ceiling the Android recorder enforces on one file.
	recordingRotateInterval = 170 * time.Second
	// recordingKeptSegments is how many of the latest segments a recording
	// keeps. Two always cover at least one full interval of history.
	recordingKeptSegments = 2
	// recordingControlTimeout bounds one short control command.
	recordingControlTimeout = 15 * time.Second
	// recordingFinalizeTimeout bounds how long a stopped recorder may take
	// to finish its file before it is killed.
	recordingFinalizeTimeout = 10 * time.Second
)

// rotationWaiter blocks until the next rotation is due and reports true,
// or reports false when ctx ends first.
type rotationWaiter func(ctx context.Context) bool

func waitRotationInterval(ctx context.Context) bool {
	timer := time.NewTimer(recordingRotateInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// segmentTempName is the dot-prefixed temporary file name of segment
// index of the recording called name.
func segmentTempName(name string, index int) string {
	return fmt.Sprintf(".%s-%03d.mp4", name, index)
}

// rotation runs a recording's rotate step on a timer in its own goroutine.
type rotation struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// startRotation calls rotate with parent each time wait reports an interval
// has elapsed, until stop is called or rotate fails. Stopping ends the wait
// only, never a rotate in flight, so parent must outlive the recording. A failed rotate ends the
// loop and its error is reported by stop.
func startRotation(parent context.Context, wait rotationWaiter, rotate func(context.Context) error) *rotation {
	ctx, cancel := context.WithCancel(parent)
	r := &rotation{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for wait(ctx) {
			if err := rotate(parent); err != nil {
				r.err = err
				return
			}
		}
	}()
	return r
}

// stop ends the loop, waits for an in-flight rotate to return, and
// reports the error that ended the loop, if any. It is safe to call more
// than once.
func (r *rotation) stop() error {
	r.cancel()
	<-r.done
	return r.err
}

var (
	errRecordingStopped   = errors.New("the recording has already been stopped")
	errRecordingDiscarded = errors.New("the recording has been discarded")
)

type recordingState int

const (
	recordingRunning recordingState = iota
	recordingStopped
	recordingDiscarded
)

func removeAll(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
