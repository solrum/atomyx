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
	// BaseDir resolves a relative file in a breakpoint location a viewer
	// sets.
	BaseDir string
	// Breakpoints are set before the run starts.
	Breakpoints []runner.BreakLocation
	// Scripts lists the scripts the run executes, so a location a viewer
	// sets is checked against their authored steps. Nil skips the check.
	Scripts func() []runner.BreakScript
	// Finder backs the advisory check on resume. Nil skips the check.
	Finder Finder
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
	actFiles    []string
	actRunning  bool
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

// SetBreakpoints implements session.Debugger.
func (a *Adapter) SetBreakpoints(locations []string) error {
	parsed := make([]runner.BreakLocation, 0, len(locations))
	for _, spec := range locations {
		loc, err := runner.ParseBreakLocation(spec, a.opts.BaseDir)
		if err != nil {
			return err
		}
		parsed = append(parsed, loc)
	}
	if a.opts.Scripts != nil {
		if err := runner.ValidateBreakLocations(parsed, a.opts.Scripts()); err != nil {
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[string]bool, len(parsed))
	for _, loc := range parsed {
		next[loc.String()] = true
	}
	for loc := range a.hit {
		if !next[loc] {
			delete(a.hit, loc)
		}
	}
	a.breakpoints = next
	return nil
}

// Pause implements session.Debugger.
func (a *Adapter) Pause() error {
	a.mu.Lock()
	a.pauseNext = true
	a.mu.Unlock()
	return nil
}

// Resume implements session.Debugger.
func (a *Adapter) Resume(ctx context.Context, opts session.ResumeOptions) (session.ResumeResult, error) {
	state := a.current()
	if state == nil {
		return session.ResumeResult{}, errNotPaused()
	}
	if !opts.Force {
		if reason := a.missReason(ctx, state.step); reason != "" {
			return session.ResumeResult{MissReason: reason}, nil
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.paused != state {
		return session.ResumeResult{}, errNotPaused()
	}
	a.paused = nil
	a.stopNext = opts.Step
	close(state.wake)
	return session.ResumeResult{Resumed: true}, nil
}

// Variables implements session.Debugger.
func (a *Adapter) Variables() (session.VariablesView, error) {
	state := a.current()
	if state == nil {
		return session.VariablesView{}, errNotPaused()
	}
	return session.VariablesView{Variables: state.run.Variables().Redacted()}, nil
}

func (a *Adapter) current() *pauseState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

func errNotPaused() error { return diag.NewHolderBusy("the run is not paused") }

// SetFinder sets the finder behind the advisory check on resume. A host
// that learns its device after it built the adapter calls it once the
// device is open.
func (a *Adapter) SetFinder(f Finder) {
	a.mu.Lock()
	a.opts.Finder = f
	a.mu.Unlock()
}
