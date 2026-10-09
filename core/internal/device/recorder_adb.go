package device

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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
}

func (r adbRecorder) Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error) {
	// Segment processes outlive this call, so they run on a context that
	// keeps the values of ctx but not its cancellation.
	runCtx := context.WithoutCancel(ctx)
	rec := &adbRecording{
		deps:   r.deps,
		serial: desc.ID,
		dir:    dir,
		name:   name,
	}
	prepare := fmt.Sprintf("rm -rf %[1]s && mkdir -p %[1]s", adbRecordingDir)
	if err := rec.shell(ctx, prepare); err != nil {
		return nil, fmt.Errorf("prepare the device recording directory: %w", err)
	}
	if err := rec.startSegment(runCtx); err != nil {
		_ = rec.clearDevice(runCtx)
		return nil, err
	}
	rec.rotation = startRotation(runCtx, r.wait, rec.rotate)
	return rec, nil
}

type adbSegment struct {
	index   int
	file    string
	pid     int
	proc    process.BackgroundProcess
	done    chan struct{}
	stopped bool
}

// adbRecording is one running Android recording.
type adbRecording struct {
	deps     RecorderDeps
	serial   string
	dir      string
	name     string
	rotation *rotation

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
	ctx, cancel := context.WithTimeout(ctx, recordingControlTimeout)
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
func (r *adbRecording) startSegment(ctx context.Context) error {
	index := r.next
	file := fmt.Sprintf("%s/segment-%03d.mp4", adbRecordingDir, index)
	command := fmt.Sprintf("echo $$; exec screenrecord --bit-rate %d %s", adbRecordingBitRate, file)
	proc, err := r.deps.Background.Start(ctx, "adb", r.adb("shell", command)...)
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
	select {
	case <-seg.done:
		return nil
	case <-time.After(recordingFinalizeTimeout):
		_ = seg.proc.Kill()
		return errors.Join(errors.New("the screen recorder did not finish in time"), signalErr)
	}
}

func (r *adbRecording) stopSegments(ctx context.Context, kill bool) error {
	var first error
	for _, seg := range r.segments {
		if err := r.stopSegment(ctx, seg, kill); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// rotate starts the next segment before it stops the current one, because
// the device records two at once and no frame is lost at the seam. It then
// drops segments older than the kept ones.
func (r *adbRecording) rotate(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != recordingRunning {
		return nil
	}
	current := r.segments[len(r.segments)-1]
	if err := r.startSegment(ctx); err != nil {
		return err
	}
	if err := r.stopSegment(ctx, current, false); err != nil {
		return err
	}
	for len(r.segments) > recordingKeptSegments {
		_ = r.control(ctx, "rm -f "+r.segments[0].file)
		r.segments = r.segments[1:]
	}
	return nil
}

func (r *adbRecording) pull(ctx context.Context) ([]string, error) {
	var files []string
	for _, seg := range r.segments {
		local := filepath.Join(r.dir, segmentTempName(r.name, seg.index))
		res, err := r.deps.Runner.Run(ctx, "adb", r.adb("pull", seg.file, local)...)
		if err != nil {
			removeAll(append(files, local))
			return nil, fmt.Errorf("pull a recorded segment: %w: %s", err, strings.TrimSpace(res.Stderr))
		}
		files = append(files, local)
	}
	return files, nil
}

func (r *adbRecording) Stop(ctx context.Context) ([]string, error) {
	rotateErr := r.rotation.stop()
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
	detached := context.WithoutCancel(ctx)
	stopErr := r.stopSegments(detached, cancelled)
	failure := errors.Join(rotateErr, stopErr)
	if cancelled {
		failure = errors.Join(ctx.Err(), failure)
	}
	if failure != nil {
		_ = r.clearDevice(detached)
		return nil, failure
	}
	files, err := r.pull(ctx)
	_ = r.clearDevice(detached)
	if err != nil {
		return nil, err
	}
	r.files = files
	return files, nil
}

func (r *adbRecording) Discard(ctx context.Context) error {
	_ = r.rotation.stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == recordingRunning {
		_ = r.stopSegments(context.WithoutCancel(ctx), ctx.Err() != nil)
	}
	r.state = recordingDiscarded
	removeAll(r.files)
	r.files = nil
	return r.clearDevice(context.WithoutCancel(ctx))
}
