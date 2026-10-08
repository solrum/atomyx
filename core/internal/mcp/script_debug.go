package mcp

import (
	"context"
	"errors"
	"sync"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/debugrun"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
)

const (
	// pauseProgressIntervalMs is how often a paused run_script tells its
	// client that the call is alive, so a client that gives up on a silent
	// request does not abandon a run a person is debugging.
	pauseProgressIntervalMs int64 = 10_000
	pausedProgressMessage         = "Paused while a person debugs the run"
)

// deviceLostWhilePausedMessage is what the agent reads when the device
// went away under a paused run.
const (
	deviceLostWhilePausedMessage = "The device was lost or disconnected while the run was paused."
	deviceLostWhilePausedRemedy  = "Select the device again and rerun the script."
)

// scriptDebugHost is the session.Debugger of an MCP session. A session
// outlives any one script, so the host delegates to the debugger of the
// run_script in flight and answers that nothing is running between
// scripts. It is safe for concurrent use.
type scriptDebugHost struct {
	mu  sync.Mutex
	run *scriptDebugRun
}

var _ session.Debugger = (*scriptDebugHost)(nil)

func errNoScriptRunning() error { return diag.NewHolderBusy("no script is running") }

// begin makes run the one viewers debug. A second script that starts
// while another is being debugged is not debuggable, and begin reports
// false for it.
func (h *scriptDebugHost) begin(run *scriptDebugRun) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.run != nil {
		return false
	}
	h.run = run
	return true
}

func (h *scriptDebugHost) end(run *scriptDebugRun) {
	h.mu.Lock()
	if h.run == run {
		h.run = nil
	}
	h.mu.Unlock()
	run.endPause()
}

// lastViewerLeft resumes the paused run, because nobody is left to
// resume it. It runs on a viewer's connection goroutine, so it does not
// wait for the resume.
func (h *scriptDebugHost) lastViewerLeft() {
	if run := h.current(); run != nil {
		go run.autoResume(run.ctx)
	}
}

func (h *scriptDebugHost) current() *scriptDebugRun {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.run
}

// SetBreakpoints implements session.Debugger.
func (h *scriptDebugHost) SetBreakpoints(locations []string) error {
	if run := h.current(); run != nil {
		return run.adapter.SetBreakpoints(locations)
	}
	return errNoScriptRunning()
}

// Pause implements session.Debugger.
func (h *scriptDebugHost) Pause() error {
	if run := h.current(); run != nil {
		return run.adapter.Pause()
	}
	return errNoScriptRunning()
}

// Resume implements session.Debugger.
func (h *scriptDebugHost) Resume(ctx context.Context, opts session.ResumeOptions) (session.ResumeResult, error) {
	if run := h.current(); run != nil {
		return run.adapter.Resume(ctx, opts)
	}
	return session.ResumeResult{}, errNoScriptRunning()
}

// Act implements session.Debugger.
func (h *scriptDebugHost) Act(ctx context.Context, stepYAML string) (session.ActResult, error) {
	run := h.current()
	if run == nil {
		return session.ActResult{}, errNoScriptRunning()
	}
	return run.adapter.Act(ctx, stepYAML)
}

// Variables implements session.Debugger.
func (h *scriptDebugHost) Variables() (session.VariablesView, error) {
	if run := h.current(); run != nil {
		return run.adapter.Variables()
	}
	return session.VariablesView{}, errNoScriptRunning()
}

// scriptDebugRun is the debugging of one run_script call. It is the
// call's runner.StepGate, and it is the debugrun.Pauser that marks the
// held session paused, keeps the call's tool budget stopped, and sends
// progress while the run waits.
type scriptDebugRun struct {
	adapter *debugrun.Adapter
	sess    *session.Session
	timers  clock.Clock
	// pauseTimeoutMs is how long the run stays paused before it resumes
	// on its own.
	pauseTimeoutMs int64
	// ctx is the call's context, which carries its tool budget and the
	// client's progress token.
	ctx context.Context

	mu           sync.Mutex
	stopWatch    context.CancelFunc
	gateEnded    bool
	resumeBudget func()
}

var (
	_ runner.StepGate = (*scriptDebugRun)(nil)
	_ debugrun.Pauser = (*scriptDebugRun)(nil)
)

// beginScriptDebug starts debugging the script def, run by orch under
// ctx. It returns nil and a no-op when the session holds no device or
// another script is already being debugged. The returned function must
// run when the script ends.
func (s *DeviceSession) beginScriptDebug(ctx context.Context, orch *orchestra.Orchestra, def *script.ScriptDefinition) (*scriptDebugRun, func()) {
	s.mu.Lock()
	held := s.sess
	s.mu.Unlock()
	if held == nil {
		return nil, func() {}
	}
	run := &scriptDebugRun{sess: held, timers: s.timers, pauseTimeoutMs: s.timeouts.DebugPauseMs, ctx: ctx}
	run.adapter = debugrun.New(debugrun.Options{
		Scripts: func() []runner.BreakScript { return []runner.BreakScript{{Definition: def}} },
		Finder:  orch,
	})
	run.adapter.Bind(run)
	if !s.debug.begin(run) {
		return nil, func() {}
	}
	return run, func() { s.debug.end(run) }
}

// BeforeStep implements runner.StepGate. The pause ends whenever the
// adapter returns, whatever the reason.
func (r *scriptDebugRun) BeforeStep(ctx context.Context, step runner.GateStep, run runner.PausedRun) error {
	err := r.adapter.BeforeStep(ctx, step, run)
	r.endPause()
	if err != nil {
		r.mu.Lock()
		r.gateEnded = true
		r.mu.Unlock()
	}
	return err
}

// EnterPause implements debugrun.Pauser.
func (r *scriptDebugRun) EnterPause(loc session.PauseLocation) {
	r.sess.EnterPause(loc)
	r.beginPause()
}

// LeavePause implements debugrun.Pauser.
func (r *scriptDebugRun) LeavePause() {
	r.sess.LeavePause()
	r.endPause()
}

// Closed implements debugrun.Pauser.
func (r *scriptDebugRun) Closed() <-chan struct{} { return r.sess.Closed() }

// Lost implements debugrun.Pauser.
func (r *scriptDebugRun) Lost() <-chan struct{} { return r.sess.Lost() }

// beginPause stops the call's tool budget and starts the progress ticker.
func (r *scriptDebugRun) beginPause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopWatch != nil {
		return
	}
	r.resumeBudget = pauseToolBudget(r.ctx)
	watchCtx, cancel := context.WithCancel(r.ctx)
	r.stopWatch = cancel
	go r.watchPause(watchCtx)
}

// endPause restarts the tool budget and stops the ticker. It is a no-op
// when the run is not paused.
func (r *scriptDebugRun) endPause() {
	r.mu.Lock()
	stop, resume := r.stopWatch, r.resumeBudget
	r.stopWatch, r.resumeBudget = nil, nil
	r.mu.Unlock()
	if stop != nil {
		stop()
		resume()
	}
}

// watchPause reports progress every pauseProgressIntervalMs and resumes
// the run once it has been paused for pauseTimeoutMs, until ctx ends.
func (r *scriptDebugRun) watchPause(ctx context.Context) {
	var pausedMs int64
	for beat := float64(1); r.timers.Sleep(ctx, pauseProgressIntervalMs) == nil; beat++ {
		pausedMs += pauseProgressIntervalMs
		if pausedMs >= r.pauseTimeoutMs {
			r.autoResume(ctx)
			return
		}
		reportPhase(ctx, acquirePhase{step: beat, message: pausedProgressMessage})
	}
}

// autoResume resumes the run without the advisory check, because no
// person is there to answer it.
func (r *scriptDebugRun) autoResume(ctx context.Context) {
	if _, paused := r.sess.Paused(); !paused {
		return
	}
	_, _ = r.sess.Resume(context.WithoutCancel(ctx), session.ResumeOptions{Force: true})
}

// endedByDeviceLoss reports that the run's gate ended because the device
// was lost or disconnected, not because a person stopped the call.
func (r *scriptDebugRun) endedByDeviceLoss() bool {
	r.mu.Lock()
	ended := r.gateEnded
	r.mu.Unlock()
	return ended && errors.Is(context.Cause(r.ctx), session.ErrDeviceLost) || r.sess.LostErr() != nil
}

// deviceLostResult is the result of a run whose device went away while it
// was paused.
func deviceLostResult(name string) runner.ScriptResult {
	return runner.ScriptResult{ScriptName: name, Abort: &runner.RunAbort{
		Reason:  runner.RunAbortDeviceLost,
		Code:    diag.CodeAgentUnreachable,
		Message: deviceLostWhilePausedMessage,
		Remedy:  deviceLostWhilePausedRemedy,
	}}
}
