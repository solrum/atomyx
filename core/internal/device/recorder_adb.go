package device

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

const (
	// adbRecordingDir is the one device directory every recording
	// writes into. The lease allows a single holder per device, so a
	// recording clears it when it starts without disturbing anyone.
	adbRecordingDir = "/data/local/tmp/atomyx-recording"
	// adbRecordingBitRate is the encoder rate in bits per second. Constant
	// scrolling wrote about 49 MB a minute at the device default and about
	// 28 MB at 4 Mbps with sharp text, while 2 Mbps blocked moving text.
	adbRecordingBitRate = 4_000_000
	// adbPidTimeout bounds how long a starting segment may take to
	// report its device-side process id.
	adbPidTimeout = 5 * time.Second
)

// adbRecorder records an Android device or emulator with the
// screenrecord tool the device ships, driven through adb.
type adbRecorder struct {
	deps RecorderDeps
	wait rotationWaiter
	// controlTimeout overrides recordingControlTimeout when positive.
	controlTimeout time.Duration
	// finalizeTimeout overrides recordingFinalizeTimeout when positive.
	finalizeTimeout time.Duration
}

func (r adbRecorder) Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error) {
	// Segment processes outlive this call, so they run on a context that
	// keeps the values of ctx but not its cancellation.
	runCtx := context.WithoutCancel(ctx)
	rec := &adbRecording{
		deps:    r.deps,
		procCtx: runCtx,
		serial:  desc.ID,
		dir:     dir,
		name:    name,
	}
	rec.controlTimeout = recordingControlTimeout
	if r.controlTimeout > 0 {
		rec.controlTimeout = r.controlTimeout
	}
	rec.finalizeTimeout = recordingFinalizeTimeout
	if r.finalizeTimeout > 0 {
		rec.finalizeTimeout = r.finalizeTimeout
	}
	prepare := fmt.Sprintf("rm -rf %[1]s && mkdir -p %[1]s", adbRecordingDir)
	if err := rec.control(ctx, prepare); err != nil {
		return nil, fmt.Errorf("prepare the device recording directory: %w", err)
	}
	if err := rec.startSegment(); err != nil {
		_ = rec.clearDevice(runCtx)
		return nil, err
	}
	rec.rotation = startRotation(runCtx, r.wait, rec.rotate, rec.logRotationFailure)
	return rec, nil
}

type adbSegment struct {
	index   int
	file    string
	pid     int
	proc    process.BackgroundProcess
	done    chan struct{}
	stopped bool
	// err is why the segment did not finish cleanly, or nil when its file
	// is playable.
	err error
}

// adbRecording is one running Android recording.
type adbRecording struct {
	deps RecorderDeps
	// procCtx keeps the values of the Start context but not its
	// cancellation, because segment processes outlive Start.
	procCtx  context.Context
	serial   string
	dir      string
	name     string
	rotation *rotation
	// controlTimeout bounds one short device command.
	controlTimeout time.Duration
	// finalizeTimeout bounds how long a stopped segment may take to
	// finish its file.
	finalizeTimeout time.Duration

	mu       sync.Mutex
	state    recordingState
	next     int
	segments []*adbSegment
	files    []string
}

func (r *adbRecording) adb(args ...string) []string {
	return append([]string{"-s", r.serial}, args...)
}

func (r *adbRecording) shell(ctx context.Context, command string) error {
	res, err := r.deps.Runner.Run(ctx, "adb", r.adb("shell", command)...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (r *adbRecording) control(ctx context.Context, command string) error {
	ctx, cancel := context.WithTimeout(ctx, r.controlTimeout)
	defer cancel()
	return r.shell(ctx, command)
}

// clearDevice removes the device directory. A stop does not fail over its
// error, because files left behind live only until the next recording
// clears the directory when it starts.
func (r *adbRecording) clearDevice(ctx context.Context) error {
	return r.control(ctx, "rm -rf "+adbRecordingDir)
}

// startSegment launches the next screenrecord process. The shell prints
// its own process id before it replaces itself with screenrecord, so the
// id names the recorder and a later interrupt reaches only that process.
func (r *adbRecording) startSegment() error {
	index := r.next
	file := fmt.Sprintf("%s/segment-%03d.mp4", adbRecordingDir, index)
	command := fmt.Sprintf("echo $$; exec screenrecord --bit-rate %d %s", adbRecordingBitRate, file)
	proc, err := r.deps.Background.Start(r.procCtx, "adb", r.adb("shell", command)...)
	if err != nil {
		return fmt.Errorf("start the screen recorder: %w", err)
	}
	pid, err := readDevicePid(proc)
	if err != nil {
		_ = proc.Kill()
		return err
	}
	seg := &adbSegment{index: index, file: file, pid: pid, proc: proc, done: make(chan struct{})}
	go func() {
		_, _ = proc.Wait()
		close(seg.done)
	}()
	r.next++
	r.segments = append(r.segments, seg)
	return nil
}

// readDevicePid reads the process id line a starting segment prints, then
// keeps draining the rest of its output so the pipe never fills.
func readDevicePid(proc process.BackgroundProcess) (int, error) {
	type result struct {
		pid int
		err error
	}
	found := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(proc.Stdout())
		line, err := reader.ReadString('\n')
		pid, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || pid <= 0 {
			found <- result{err: fmt.Errorf("the screen recorder reported no process id: %w", errors.Join(err, convErr))}
			return
		}
		found <- result{pid: pid}
		_, _ = io.Copy(io.Discard, reader)
	}()
	select {
	case res := <-found:
		return res.pid, res.err
	case <-time.After(adbPidTimeout):
		return 0, errors.New("the screen recorder did not report a process id in time")
	}
}

// stopSegment ends one segment. With kill false it interrupts the device
// process and waits for the recorder to finalize its file. With kill true
// it ends the host connection without waiting, because the file is lost.
// Both the control command and the finalize wait end with ctx.
func (r *adbRecording) stopSegment(ctx context.Context, seg *adbSegment, kill bool) error {
	if seg.stopped {
		return nil
	}
	seg.stopped = true
	select {
	case <-seg.done:
		return nil
	default:
	}
	signalErr := r.control(ctx, fmt.Sprintf("kill -INT %d", seg.pid))
	if kill {
		return seg.proc.Kill()
	}
	wait, cancel := context.WithTimeout(ctx, r.finalizeTimeout)
	defer cancel()
	select {
	case <-seg.done:
		return nil
	case <-wait.Done():
		_ = seg.proc.Kill()
		return errors.Join(errors.New("the screen recorder did not finish in time"), signalErr)
	}
}

// stopSegments ends every segment, records on each why it failed if it
// did, and returns the failures.
func (r *adbRecording) stopSegments(ctx context.Context, kill bool) error {
	var failures []error
	for _, seg := range r.segments {
		if err := r.stopSegment(ctx, seg, kill); err != nil {
			seg.err = err
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *adbRecording) logRotationFailure(err error) {
	r.deps.logger().Warn("rotating the screen recording segment failed, retrying at the next interval", logging.LogContext{"error": err.Error()})
}

// dropSegment forgets seg and removes its device file.
func (r *adbRecording) dropSegment(ctx context.Context, seg *adbSegment) {
	for i, s := range r.segments {
		if s == seg {
			r.segments = append(r.segments[:i], r.segments[i+1:]...)
			break
		}
	}
	_ = r.control(ctx, "rm -f "+seg.file)
}

// rotate starts the next segment before it stops the current one, because
// the device records two at once and no frame is lost at the seam. A
// current segment that does not finish cleanly is dropped, and segments
// older than the kept ones are dropped as well. A failure leaves the live
// segment running, so the next rotation can try again.
func (r *adbRecording) rotate(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != recordingRunning {
		return nil
	}
	current := r.segments[len(r.segments)-1]
	if err := r.startSegment(); err != nil {
		return err
	}
	if err := r.stopSegment(ctx, current, false); err != nil {
		r.dropSegment(ctx, current)
		return err
	}
	for len(r.segments) > recordingKeptSegments {
		_ = r.control(ctx, "rm -f "+r.segments[0].file)
		r.segments = r.segments[1:]
	}
	return nil
}

// pull copies the segments that finished cleanly to dir. A segment that
// cannot be pulled is skipped and reported, so one bad file does not take
// the playable ones with it.
func (r *adbRecording) pull(ctx context.Context) ([]string, error) {
	var files []string
	var failures []error
	for _, seg := range r.segments {
		if seg.err != nil {
			failures = append(failures, seg.err)
			continue
		}
		local := filepath.Join(r.dir, segmentTempName(r.name, seg.index))
		res, err := r.deps.Runner.Run(ctx, "adb", r.adb("pull", seg.file, local)...)
		if err != nil {
			_ = os.Remove(local)
			failures = append(failures, fmt.Errorf("pull a recorded segment: %w: %s", err, strings.TrimSpace(res.Stderr)))
			continue
		}
		files = append(files, local)
	}
	if len(files) == 0 {
		return nil, errors.Join(append([]error{errNoPlayableSegment}, failures...)...)
	}
	return files, nil
}

func (r *adbRecording) Stop(ctx context.Context) ([]string, error) {
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
	cleanup, release := cleanupContext(ctx)
	defer release()
	cancelled := ctx.Err() != nil
	stopCtx := ctx
	if cancelled {
		stopCtx = cleanup
	}
	stopErr := r.stopSegments(stopCtx, cancelled)
	if cancelled {
		_ = r.clearDevice(cleanup)
		return nil, errors.Join(ctx.Err(), stopErr)
	}
	files, err := r.pull(ctx)
	_ = r.clearDevice(cleanup)
	if err != nil {
		return nil, err
	}
	r.files = files
	return files, nil
}

// Discard ends a running recording by killing it without waiting for the
// recorder to finalize its file, because every file is deleted anyway.
func (r *adbRecording) Discard(ctx context.Context) error {
	r.rotation.stop(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	cleanup, release := cleanupContext(ctx)
	defer release()
	if r.state == recordingRunning {
		_ = r.stopSegments(cleanup, true)
	}
	r.state = recordingDiscarded
	removeAll(r.files)
	r.files = nil
	return r.clearDevice(cleanup)
}
