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
	// recordingCleanupTimeout bounds cleanup that runs after its caller's
	// context was cancelled and carries no deadline of its own.
	recordingCleanupTimeout = 10 * time.Second
)

// cleanupContext returns a context for work that must still run after ctx
// is cancelled, such as ending a recorder process and clearing the device.
// It drops the cancellation of ctx but keeps its deadline, so a caller's
// time bound holds for the whole teardown. A ctx without a deadline gets
// recordingCleanupTimeout instead, so cleanup never hangs.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(detached, deadline)
	}
	return context.WithTimeout(detached, recordingCleanupTimeout)
}

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
	cancelWait context.CancelFunc
	abort      context.CancelFunc
	done       chan struct{}
	err        error
}

// startRotation calls rotate each time wait reports an interval has
// elapsed, until stop is called or rotate fails. The context handed to
// rotate derives from parent and is cancelled only when stop gives up
// waiting for a rotate in flight, so parent must outlive the recording. A
// failed rotate ends the loop and its error is reported by stop.
func startRotation(parent context.Context, wait rotationWaiter, rotate func(context.Context) error) *rotation {
	waitCtx, cancelWait := context.WithCancel(parent)
	rotateCtx, abort := context.WithCancel(parent)
	r := &rotation{cancelWait: cancelWait, abort: abort, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for wait(waitCtx) {
			if err := rotate(rotateCtx); err != nil {
				r.err = err
				return
			}
		}
	}()
	return r
}

// stop ends the loop and reports the error that ended it, if any. It
// lets a rotate in flight finish until ctx ends, then aborts that rotate
// and waits for it to return, so a caller's time bound holds. It is safe
// to call more than once.
func (r *rotation) stop(ctx context.Context) error {
	r.cancelWait()
	select {
	case <-r.done:
	case <-ctx.Done():
		r.abort()
		<-r.done
	}
	r.abort()
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
