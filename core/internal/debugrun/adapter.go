package debugrun

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
)

// Pauser is the session side of a pause. *session.Session satisfies it.
// A host that prints a prompt wraps the session and forwards the calls.
type Pauser interface {
	EnterPause(loc session.PauseLocation)
	LeavePause()
	Closed() <-chan struct{}
	Lost() <-chan struct{}
}

// Options configures an Adapter. The zero value is usable and debugs a
// run with no breakpoints.
type Options struct {
	// Breakpoints are set before the run starts.
	Breakpoints []runner.BreakLocation
}

// Adapter implements runner.StepGate and session.Debugger over one run
// host. It is safe for concurrent use.
type Adapter struct {
	opts Options

	mu          sync.Mutex
	pauser      Pauser
	breakpoints map[string]bool
	hit         map[string]bool
	pauseNext   bool
	stopNext    bool
	lastSource  string
	paused      *pauseState
}

// pauseState is the run blocked at the gate.
type pauseState struct {
	run  runner.PausedRun
	step runner.GateStep
	wake chan struct{}
}

var (
	_ runner.StepGate = (*Adapter)(nil)

	errSessionClosed = errors.New("the session closed while the run was paused")
	errDeviceLost    = errors.New("the device was lost while the run was paused")
)

// New returns an Adapter. Bind must be called before the run starts, or
// the adapter never stops.
func New(opts Options) *Adapter {
	a := &Adapter{opts: opts, breakpoints: map[string]bool{}, hit: map[string]bool{}}
	for _, loc := range opts.Breakpoints {
		a.breakpoints[loc.String()] = true
	}
	return a
}

// Bind sets the session the adapter pauses.
func (a *Adapter) Bind(p Pauser) {
	a.mu.Lock()
	a.pauser = p
	a.mu.Unlock()
}

// Unhit lists, sorted, the breakpoints that never stopped the run.
func (a *Adapter) Unhit() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for loc := range a.breakpoints {
		if !a.hit[loc] {
			out = append(out, loc)
		}
	}
	sort.Strings(out)
	return out
}

// BeforeStep implements runner.StepGate. It blocks while the run is
// paused and returns an error when the pause ends for any reason other
// than a resume, which ends the run as cancelled.
func (a *Adapter) BeforeStep(ctx context.Context, step runner.GateStep, run runner.PausedRun) error {
	a.mu.Lock()
	pauser := a.pauser
	atBreakpoint := step.Depth == 0 && step.Location != "" && a.breakpoints[step.Location]
	if step.Depth == 0 && step.Location != "" {
		a.lastSource, _ = splitLocation(step.Location)
	}
	if pauser == nil || (!atBreakpoint && !a.pauseNext && !a.stopNext) {
		a.mu.Unlock()
		return nil
	}
	if atBreakpoint {
		a.hit[step.Location] = true
	}
	a.pauseNext, a.stopNext = false, false
	state := &pauseState{run: run, step: step, wake: make(chan struct{})}
	a.paused = state
	loc := a.pauseLocation(step)
	a.mu.Unlock()

	pauser.EnterPause(loc)
	var err error
	select {
	case <-state.wake:
	case <-ctx.Done():
		err = ctx.Err()
	case <-pauser.Closed():
		err = errSessionClosed
	case <-pauser.Lost():
		err = errDeviceLost
	}
	if err != nil {
		a.mu.Lock()
		if a.paused == state {
			a.paused = nil
		}
		a.mu.Unlock()
		pauser.LeavePause()
	}
	return err
}

// pauseLocation names where step stands. A nested step has no line, so
// it reports the file of the top-level step that holds it.
func (a *Adapter) pauseLocation(step runner.GateStep) session.PauseLocation {
	source, line := a.lastSource, 0
	if step.Location != "" {
		source, line = splitLocation(step.Location)
	}
	return session.PauseLocation{Source: source, Line: line, Depth: step.Depth, Command: step.Command}
}

func splitLocation(location string) (string, int) {
	cut := strings.LastIndex(location, ":")
	line, _ := strconv.Atoi(location[cut+1:])
	return location[:cut], line
}

// Pause implements session.Debugger.
func (a *Adapter) Pause() error {
	a.mu.Lock()
	a.pauseNext = true
	a.mu.Unlock()
	return nil
}

// Resume implements session.Debugger.
func (a *Adapter) Resume(_ context.Context, opts session.ResumeOptions) (session.ResumeResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.paused
	if state == nil {
		return session.ResumeResult{}, errNotPaused()
	}
	a.paused = nil
	a.stopNext = opts.Step
	close(state.wake)
	return session.ResumeResult{Resumed: true}, nil
}

func errNotPaused() error { return diag.NewHolderBusy("the run is not paused") }
