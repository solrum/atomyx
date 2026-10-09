package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

// simulatorRecordingCodec is the video codec simctl encodes with. Constant
// scrolling wrote about 108 MB a minute with h264 and about 59 MB a minute
// with hevc, with text equally sharp in a still frame, so hevc roughly
// halves the disk a kept window of two segments needs.
const simulatorRecordingCodec = "hevc"

// simulatorRecorder records an iOS simulator with xcrun simctl. The tool
// writes the file on the host, so a recording needs no pull and no
// device-side cleanup.
type simulatorRecorder struct {
	deps RecorderDeps
	wait rotationWaiter
	// orphanGrace overrides recordingFinalizeTimeout as the time a
	// leftover recorder gets to exit when positive.
	orphanGrace time.Duration
}

func (r simulatorRecorder) Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error) {
	// Segment processes outlive this call, so they run on a context that
	// keeps the values of ctx but not its cancellation.
	runCtx := context.WithoutCancel(ctx)
	rec := &simulatorRecording{
		deps:    r.deps,
		procCtx: runCtx,
		udid:    desc.ID,
		dir:     dir,
		name:    name,
	}
	if err := r.reapOrphans(ctx, desc.ID); err != nil {
		return nil, err
	}
	if err := rec.startSegment(); err != nil {
		return nil, err
	}
	rec.rotation = startRotation(runCtx, r.wait, rec.rotate, rec.logRotationFailure)
	return rec, nil
}

// reapOrphans clears recorders a dead atomyx process left on the
// simulator, within the bound of the Start context.
func (r simulatorRecorder) reapOrphans(ctx context.Context, udid string) error {
	grace := recordingFinalizeTimeout
	if r.orphanGrace > 0 {
		grace = r.orphanGrace
	}
	ctx, cancel := context.WithTimeout(ctx, recordingControlTimeout+2*grace)
	defer cancel()
	return simulatorOrphanReaper{runner: r.deps.Runner, grace: grace}.reap(ctx, udid)
}

type simulatorSegment struct {
	file    string
	proc    process.BackgroundProcess
	done    chan struct{}
	result  process.BackgroundResult
	stopped bool
	// err is why the segment did not finish cleanly, or nil when its file
	// is playable.
	err error
}

// simulatorRecording is one running simulator recording.
type simulatorRecording struct {
	deps RecorderDeps
	// procCtx keeps the values of the Start context but not its
	// cancellation, because segment processes outlive Start.
	procCtx  context.Context
	udid     string
	dir      string
	name     string
	rotation *rotation

	mu       sync.Mutex
	state    recordingState
	next     int
	segments []*simulatorSegment
	files    []string
}

func (r *simulatorRecording) startSegment() error {
	file := filepath.Join(r.dir, segmentTempName(r.name, r.next))
	proc, err := r.deps.Background.Start(r.procCtx, "xcrun", "simctl", "io", r.udid, "recordVideo",
		"--codec", simulatorRecordingCodec, "--force", file)
	if err != nil {
		return fmt.Errorf("start the simulator recorder: %w", err)
	}
	seg := &simulatorSegment{file: file, proc: proc, done: make(chan struct{})}
	go func() {
		seg.result, _ = proc.Wait()
		close(seg.done)
	}()
	r.next++
	r.segments = append(r.segments, seg)
	return nil
}

// stopSegment ends one segment. With kill false it interrupts the
// recorder so it finalizes its file, and with kill true it ends it
// without regard for the file. A recorder that had already exited with a
// failure before the stop is reported with its error output. The
// interrupt wait ends with ctx, and a recorder still running then is
// killed.
func (r *simulatorRecording) stopSegment(ctx context.Context, seg *simulatorSegment, kill bool) error {
	if seg.stopped {
		return nil
	}
	seg.stopped = true
	select {
	case <-seg.done:
		if seg.result.ExitCode != 0 {
			return fmt.Errorf("the simulator recorder exited with status %d: %s", seg.result.ExitCode, strings.TrimSpace(seg.result.Stderr))
		}
		return nil
	default:
	}
	if kill {
		return seg.proc.Kill()
	}
	interrupted := make(chan error, 1)
	go func() { interrupted <- seg.proc.Interrupt(recordingFinalizeTimeout) }()
	select {
	case err := <-interrupted:
		return err
	case <-ctx.Done():
		_ = seg.proc.Kill()
		return errors.Join(ctx.Err(), process.ErrInterruptEscalated)
	}
}

// stopSegments ends every segment, records on each why it failed if it
// did, and returns the failures.
func (r *simulatorRecording) stopSegments(ctx context.Context, kill bool) error {
	var failures []error
	for _, seg := range r.segments {
		if err := r.stopSegment(ctx, seg, kill); err != nil {
			seg.err = err
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *simulatorRecording) logRotationFailure(err error) {
	r.deps.logger().Warn("rotating the screen recording segment failed, retrying at the next interval", logging.LogContext{"error": err.Error()})
}

// dropSegment forgets seg and removes its file.
func (r *simulatorRecording) dropSegment(seg *simulatorSegment) {
	for i, s := range r.segments {
		if s == seg {
			r.segments = append(r.segments[:i], r.segments[i+1:]...)
			break
		}
	}
	_ = os.Remove(seg.file)
}

// rotate stops the current segment and then starts the next one, because
// the simulator refuses a second recording while one is running. The seam
// loses a fraction of a second of video. A current segment that does not
// finish cleanly is dropped. A failure leaves the segments that finished
// cleanly in place, and the next rotation starts a new segment again.
func (r *simulatorRecording) rotate(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != recordingRunning {
		return nil
	}
	var stopErr error
	if n := len(r.segments); n > 0 && !r.segments[n-1].stopped {
		current := r.segments[n-1]
		if stopErr = r.stopSegment(ctx, current, false); stopErr != nil {
			r.dropSegment(current)
		}
	}
	startErr := r.startSegment()
	for len(r.segments) > recordingKeptSegments {
		_ = os.Remove(r.segments[0].file)
		r.segments = r.segments[1:]
	}
	return errors.Join(stopErr, startErr)
}

func (r *simulatorRecording) segmentFiles() []string {
	files := make([]string, 0, len(r.segments))
	for _, seg := range r.segments {
		files = append(files, seg.file)
	}
	return files
}

func (r *simulatorRecording) Stop(ctx context.Context) ([]string, error) {
	r.rotation.stop(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.state {
	case recordingStopped:
		return nil, errRecordingStopped
	case recordingDiscarded:
		return nil, errRecordingDiscarded
	}
	r.state = recordingStopped
	cancelled := ctx.Err() != nil
	stopErr := r.stopSegments(ctx, cancelled)
	if cancelled {
		removeAll(r.segmentFiles())
		return nil, errors.Join(ctx.Err(), stopErr)
	}
	var files []string
	failures := []error{errNoPlayableSegment}
	for _, seg := range r.segments {
		if seg.err != nil {
			_ = os.Remove(seg.file)
			failures = append(failures, seg.err)
			continue
		}
		if _, err := os.Stat(seg.file); err != nil {
			failures = append(failures, fmt.Errorf("the simulator recorder left no file: %w", err))
			continue
		}
		files = append(files, seg.file)
	}
	if len(files) == 0 {
		return nil, errors.Join(failures...)
	}
	r.files = files
	return files, nil
}

// Discard ends a running recording by killing it without waiting for the
// recorder to finalize its file, because every file is deleted anyway.
func (r *simulatorRecording) Discard(ctx context.Context) error {
	r.rotation.stop(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == recordingRunning {
		_ = r.stopSegments(ctx, true)
	}
	r.state = recordingDiscarded
	removeAll(r.segmentFiles())
	r.files = nil
	return nil
}
