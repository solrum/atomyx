package process

import (
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ExecBackground starts commands through the operating system's
// process loader without waiting for them to exit. It is the
// production BackgroundRunner.
type ExecBackground struct {
	// waitDelay overrides ioDrainDelay when positive, so a test can
	// observe the bounded-drain behavior without waiting real seconds.
	waitDelay time.Duration
}

// NewExecBackground constructs an ExecBackground runner.
func NewExecBackground() *ExecBackground {
	return &ExecBackground{}
}

var _ BackgroundRunner = (*ExecBackground)(nil)

// stderrCaptureLimit bounds how many bytes of a background process's
// standard error ExecBackground retains for BackgroundResult.Stderr.
// A verbose or endlessly chatty process must not grow memory without
// limit just because nobody has read its stderr yet.
const stderrCaptureLimit = 64 * 1024

// ioDrainDelay bounds how long Wait keeps waiting for a process's
// output pipes to close after the process itself has exited. A
// descendant that inherited the pipe and outlives its parent would
// otherwise hold Wait open for as long as it runs.
const ioDrainDelay = 2 * time.Second

// killGracePeriod bounds how long Kill waits for a SIGTERM'd process
// group to exit on its own before escalating to SIGKILL. Long enough
// for a well-behaved local dev-tooling subprocess (xcodebuild, adb,
// the atomyx-sim-hid helper) to flush and exit; short enough that a
// process ignoring SIGTERM does not stall the caller's teardown path.
const killGracePeriod = 2 * time.Second

// Start implements BackgroundRunner by invoking name with args via
// exec.CommandContext and returning as soon as the process has
// started. A canceled or expired ctx kills the process at any later
// point, including while a caller is blocked reading its stdout.
func (r ExecBackground) Start(ctx context.Context, name string, args ...string) (BackgroundProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	// Setpgid places the process in its own process group, equal to
	// its own pid, instead of inheriting this program's group. Kill
	// signals -pid to reach every grandchild the command spawns, not
	// just the direct child; without this, a grandchild the direct
	// child forked (for example a shell running a background job)
	// would survive Kill as an orphan.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = ioDrainDelay
	if r.waitDelay > 0 {
		cmd.WaitDelay = r.waitDelay
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := newCappedBuffer(stderrCaptureLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execBackgroundProcess{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

// execBackgroundProcess is the BackgroundProcess ExecBackground.Start
// returns. Wait is memoized because os/exec.Cmd.Wait panics if called
// more than once.
type execBackgroundProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *cappedBuffer

	mu      sync.Mutex
	waited  bool
	result  BackgroundResult
	waitErr error
}

func (p *execBackgroundProcess) Stdout() io.Reader { return p.stdout }

func (p *execBackgroundProcess) Wait() (BackgroundResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waited {
		return p.result, p.waitErr
	}
	p.waited = true
	p.waitErr = p.cmd.Wait()
	if p.cmd.ProcessState != nil {
		p.result.ExitCode = p.cmd.ProcessState.ExitCode()
	}
	p.result.Stderr = p.stderr.String()
	return p.result, p.waitErr
}

// Kill stops the process and every process it spawned: it signals the
// process group (started via Setpgid in Start) with SIGTERM, waits up
// to killGracePeriod for it to exit on its own, and escalates to
// SIGKILL if it has not. Kill always reaps the process itself before
// returning, on either the SIGTERM or the SIGKILL path, so a caller
// never needs a separate Wait call to avoid leaving a zombie behind.
// Killing an already-exited process is a no-op.
func (p *execBackgroundProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	pgid := p.cmd.Process.Pid

	reaped := make(chan struct{})
	go func() {
		_, _ = p.Wait()
		close(reaped)
	}()

	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-reaped:
		return nil
	case <-time.After(killGracePeriod):
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-reaped
	return nil
}

// cappedBuffer retains the last limit bytes written to it and
// silently discards everything before them, so a background process's
// stderr can be captured for diagnostics without an unbounded buffer
// while still keeping the end of the output, where a failing tool
// prints its real error. Safe for concurrent use because a write
// arriving on the process's stderr pipe and a String call from Wait
// can race.
type cappedBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) >= w.limit {
		w.buf = append(w.buf[:0], p[len(p)-w.limit:]...)
	} else {
		w.buf = append(w.buf, p...)
		if excess := len(w.buf) - w.limit; excess > 0 {
			w.buf = append(w.buf[:0], w.buf[excess:]...)
		}
	}
	// Reporting the full length written, rather than just what was
	// retained, matches io.Writer's contract that a nil error implies
	// every byte was accepted: the excess was deliberately discarded,
	// not lost to a write failure.
	return len(p), nil
}

func (w *cappedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
