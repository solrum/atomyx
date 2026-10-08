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
	// pausedProgressMessage is the progress text sent while the run waits.
	pausedProgressMessage = "Paused while a person debugs the run"
)

// What the agent reads when the device went away under a run. The lost
// pair is for a device that disappeared on its own, and the disconnected
// pair is for a device the agent itself disconnected.
const (
	deviceLostMessage               = "The device was lost while the script was running."
	deviceLostWhilePausedMessage    = "The device was lost while the run was paused."
	deviceDisconnectedMessage       = "The device was disconnected while the script was running."
	deviceDisconnectedPausedMessage = "The device was disconnected while the run was paused."
	deviceLostRemedy                = "Select the device again and rerun the script."
	deviceDisconnectedRemedy        = "Call select_device with a platform and device id from list_devices, then rerun the script."
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
	result, err := run.adapter.Act(ctx, stepYAML)
	if err == nil {
		run.recordAction(result)
	}
	return result, err
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

	mu        sync.Mutex
	actions   []humanActionWire
	stopWatch context.CancelFunc
	// gateEnded is true once the run's step gate returned an error, which
	// happens only to a run that was paused.
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

// settleDeviceLoss reports a run that ended because its device went away
// as an aborted run, keeping the steps it already ran. A run that
// finished, that the runner already classified, or that a person stopped
// is returned as it is.
func (r *scriptDebugRun) settleDeviceLoss(result runner.ScriptResult, err error) (runner.ScriptResult, error) {
	if result.Abort != nil || (err == nil && result.Ok) {
		return result, err
	}
	abort := r.deviceLossAbort()
	if abort == nil {
		return result, err
	}
	result.Ok = false
	result.Abort = abort
	return result, nil
}

// deviceLossAbort describes how the device went away, or returns nil
// when it did not. A device the session lost reports the code of its
// cause, and a device the agent disconnected tells the agent to select
// one again.
func (r *scriptDebugRun) deviceLossAbort() *runner.RunAbort {
	lostErr := r.sess.LostErr()
	if lostErr == nil && !errors.Is(context.Cause(r.ctx), session.ErrDeviceLost) {
		return nil
	}
	r.mu.Lock()
	paused := r.gateEnded
	r.mu.Unlock()
	if lostErr == nil {
		message := deviceDisconnectedMessage
		if paused {
			message = deviceDisconnectedPausedMessage
		}
		return &runner.RunAbort{
			Reason: runner.RunAbortDeviceLost, Code: diag.CodeDeviceNotFound,
			Message: message, Remedy: deviceDisconnectedRemedy,
		}
	}
	code, _, _ := diag.Describe(lostErr)
	if code == diag.CodeUnknown {
		code = diag.CodeAgentUnreachable
	}
	message := deviceLostMessage
	if paused {
		message = deviceLostWhilePausedMessage
	}
	return &runner.RunAbort{
		Reason: runner.RunAbortDeviceLost, Code: code,
		Message: message, Remedy: deviceLostRemedy,
	}
}

// humanActionWire is a step a person ran on the device while the run was
// paused, as run_script reports it to the agent. Step has the text of a
// type step masked and every secret value redacted.
type humanActionWire struct {
	Step       string   `json:"step"`
	Command    string   `json:"command"`
	Outcome    string   `json:"outcome"`
	Message    string   `json:"message,omitempty"`
	DurationMs int64    `json:"durationMs"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

func (r *scriptDebugRun) recordAction(result session.ActResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, humanActionWire{
		Step: result.Step, Command: result.Command, Outcome: result.Outcome,
		Message: result.Message, DurationMs: result.DurationMs, Artifacts: result.Artifacts,
	})
}

// humanActions lists, in order, the steps people ran while the run was
// paused. A nil run has none.
func (r *scriptDebugRun) humanActions() []humanActionWire {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]humanActionWire(nil), r.actions...)
}
