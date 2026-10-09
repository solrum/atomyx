package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// FakeBackgroundInvocation records one call made to a
// FakeBackground's Start method, in dispatch order.
type FakeBackgroundInvocation struct {
	Name string
	Args []string
	// Env is the additional environment the call's context carried
	// through WithEnv, or nil when it carried none.
	Env []string
}

// FakeBackgroundProcess is the BackgroundProcess a FakeBackground
// hands back from Start. A test calls Write to script the process's
// stdout and Exit to simulate it exiting. Write never blocks
// regardless of whether anything is currently reading, unlike a real
// OS pipe, so a test can stage output before Launch (or any other
// consumer) starts reading it.
//
// FakeBackgroundProcess is safe for concurrent use.
type FakeBackgroundProcess struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool

	in       bytes.Buffer
	inClosed bool

	pid              int
	killed           bool
	interrupted      bool
	ignoresInterrupt bool
	exited           bool
	exitCode         int
	stderr           string
}

// NewFakeBackgroundProcess constructs a FakeBackgroundProcess with
// empty, still-open stdout.
func NewFakeBackgroundProcess() *FakeBackgroundProcess {
	p := &FakeBackgroundProcess{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Write appends data to the process's simulated stdout.
func (p *FakeBackgroundProcess) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, err := p.buf.Write(data)
	p.cond.Broadcast()
	return n, err
}

// Exit simulates the process exiting with exitCode, closing its
// stdout so a blocked reader observes EOF once the already-buffered
// output is drained.
func (p *FakeBackgroundProcess) Exit(exitCode int) {
	p.ExitWithStderr(exitCode, "")
}

// ExitWithStderr simulates the process exiting with exitCode after
// writing stderr to its standard error, which Wait reports through
// BackgroundResult.Stderr.
func (p *FakeBackgroundProcess) ExitWithStderr(exitCode int, stderr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stderr = stderr
	p.closed = true
	p.exited = true
	p.exitCode = exitCode
	p.cond.Broadcast()
}

func (p *FakeBackgroundProcess) Stdout() io.Reader { return fakeBackgroundReader{p} }

// Stdin returns the writer a caller feeds the process through.
func (p *FakeBackgroundProcess) Stdin() io.WriteCloser { return fakeBackgroundInput{p} }

// Input returns a reader over what the caller wrote to Stdin. Like a real
// pipe it blocks until more data arrives, and it reports io.EOF once the
// caller closed Stdin or the process exited.
func (p *FakeBackgroundProcess) Input() io.Reader { return fakeBackgroundInputReader{p} }

type fakeBackgroundInput struct{ p *FakeBackgroundProcess }

func (w fakeBackgroundInput) Write(data []byte) (int, error) {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	if w.p.inClosed || w.p.exited {
		return 0, io.ErrClosedPipe
	}
	n, err := w.p.in.Write(data)
	w.p.cond.Broadcast()
	return n, err
}

func (w fakeBackgroundInput) Close() error {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	w.p.inClosed = true
	w.p.cond.Broadcast()
	return nil
}

type fakeBackgroundInputReader struct{ p *FakeBackgroundProcess }

func (r fakeBackgroundInputReader) Read(dst []byte) (int, error) {
	p := r.p
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.in.Len() == 0 && !p.inClosed && !p.exited {
		p.cond.Wait()
	}
	if p.in.Len() == 0 {
		return 0, io.EOF
	}
	return p.in.Read(dst)
}

// fakeBackgroundReader reads from its FakeBackgroundProcess's
// buffered stdout, blocking until more data arrives or the process
// exits, matching how reading a real pipe behaves for a caller that
// blocks on the next line.
type fakeBackgroundReader struct{ p *FakeBackgroundProcess }

func (r fakeBackgroundReader) Read(dst []byte) (int, error) {
	p := r.p
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.buf.Len() == 0 && !p.closed {
		p.cond.Wait()
	}
	if p.buf.Len() == 0 {
		return 0, io.EOF
	}
	return p.buf.Read(dst)
}

func (p *FakeBackgroundProcess) Wait() (BackgroundResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for !p.exited {
		p.cond.Wait()
	}
	result := BackgroundResult{ExitCode: p.exitCode, Stderr: p.stderr}
	if p.exitCode != 0 {
		return result, fmt.Errorf("exit status %d", p.exitCode)
	}
	return result, nil
}

// SetPid sets the process id Pid reports.
func (p *FakeBackgroundProcess) SetPid(pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pid = pid
}

// Pid reports the process id set through SetPid, or zero.
func (p *FakeBackgroundProcess) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// Killed reports whether Kill has been called.
func (p *FakeBackgroundProcess) Killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// Kill marks the process killed and, if it had not already exited,
// simulates an immediate exit with code -1 so a blocked Wait or
// Stdout read unblocks.
func (p *FakeBackgroundProcess) Kill() error {
	p.mu.Lock()
	p.killed = true
	if !p.exited {
		p.closed = true
		p.exited = true
		p.exitCode = -1
	}
	p.mu.Unlock()
	p.cond.Broadcast()
	return nil
}

// FakeInterruptExitCode is the exit code a FakeBackgroundProcess reports
// after an Interrupt it did not ignore, matching a shell's status for a
// process ended by SIGINT.
const FakeInterruptExitCode = 130

// IgnoreInterrupt makes the process behave like one that ignores
// SIGINT, so Interrupt falls back to Kill.
func (p *FakeBackgroundProcess) IgnoreInterrupt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ignoresInterrupt = true
}

// Interrupted reports whether Interrupt has been called.
func (p *FakeBackgroundProcess) Interrupted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interrupted
}

// Interrupt marks the process interrupted. A running process exits with
// FakeInterruptExitCode unless IgnoreInterrupt was called, in which case
// the call falls back to Kill and returns ErrInterruptEscalated. The
// grace period is not waited on.
func (p *FakeBackgroundProcess) Interrupt(_ time.Duration) error {
	p.mu.Lock()
	p.interrupted = true
	if p.exited {
		p.mu.Unlock()
		return nil
	}
	if p.ignoresInterrupt {
		p.mu.Unlock()
		if err := p.Kill(); err != nil {
			return err
		}
		return ErrInterruptEscalated
	}
	p.closed = true
	p.exited = true
	p.exitCode = FakeInterruptExitCode
	p.mu.Unlock()
	p.cond.Broadcast()
	return nil
}

// FakeBackground is a BackgroundRunner test double. A test stages one
// FakeBackgroundProcess per expected Start call with Stub. A call
// beyond the staged ones returns DefaultErr, or an already-exited
// process with empty stdout when DefaultErr is nil.
//
// FakeBackground is safe for concurrent use.
type FakeBackground struct {
	mu          sync.Mutex
	queue       []*FakeBackgroundProcess
	invocations []FakeBackgroundInvocation

	// DefaultErr is returned by Start once every staged process has
	// been consumed.
	DefaultErr error
}

// NewFakeBackground constructs an empty FakeBackground.
func NewFakeBackground() *FakeBackground {
	return &FakeBackground{}
}

var (
	_ BackgroundRunner  = (*FakeBackground)(nil)
	_ InteractiveRunner = (*FakeBackground)(nil)
)

// Stub appends proc to the queue of processes Start hands out, in
// order.
func (f *FakeBackground) Stub(proc *FakeBackgroundProcess) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, proc)
}

// Invocations returns every call made to Start so far, in dispatch
// order.
func (f *FakeBackground) Invocations() []FakeBackgroundInvocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeBackgroundInvocation(nil), f.invocations...)
}

func (f *FakeBackground) Start(ctx context.Context, name string, args ...string) (BackgroundProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invocations = append(f.invocations, FakeBackgroundInvocation{Name: name, Args: append([]string(nil), args...), Env: append([]string(nil), EnvFrom(ctx)...)})
	if len(f.queue) == 0 {
		if f.DefaultErr != nil {
			return nil, f.DefaultErr
		}
		proc := NewFakeBackgroundProcess()
		proc.Exit(0)
		return proc, nil
	}
	proc := f.queue[0]
	f.queue = f.queue[1:]
	return proc, nil
}

// StartInteractive implements InteractiveRunner over the same staged
// processes as Start.
func (f *FakeBackground) StartInteractive(ctx context.Context, name string, args ...string) (InteractiveProcess, error) {
	proc, err := f.Start(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	return proc.(InteractiveProcess), nil
}
