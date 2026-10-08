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
// run_script in flight. Between scripts it keeps only the breakpoint list
// and the pause request a viewer sets, and applies them to the next
// run_script before its first step, so an operator can arm a run before
// it starts. The locations are checked for syntax when set, but not
// against the script, which is not known yet: an inline location whose
// line starts no top-level step never stops the run and is not an error.
// Every other request is refused between scripts. It is safe for
// concurrent use.
type scriptDebugHost struct {
	mu  sync.Mutex
	run *scriptDebugRun
	// pendingBreakpoints and pendingPause wait for the next script.
	pendingBreakpoints []runner.BreakLocation
	pendingPause       bool
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
	run.adapter.Arm(h.pendingBreakpoints, h.pendingPause)
	h.pendingBreakpoints, h.pendingPause = nil, false
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

// lastViewerLeft drops what the viewers asked of the run and resumes the
// run if it is paused, because nobody is left to resume it. It runs on a
// viewer's connection goroutine, so it does not wait for the resume.
func (h *scriptDebugHost) lastViewerLeft() {
	h.mu.Lock()
	h.pendingBreakpoints, h.pendingPause = nil, false
	run := h.run
	h.mu.Unlock()
	if run == nil {
		return
	}
	run.adapter.ClearRequests()
	if id := run.adapter.PauseID(); id != 0 {
		go run.resumeIfAbandoned(id)
	}
}

func (h *scriptDebugHost) current() *scriptDebugRun {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.run
}

// SetBreakpoints implements session.Debugger.
func (h *scriptDebugHost) SetBreakpoints(locations []string) error {
	h.mu.Lock()
	run := h.run
	if run == nil {
		defer h.mu.Unlock()
		parsed := make([]runner.BreakLocation, 0, len(locations))
		for _, spec := range locations {
			loc, err := runner.ParseBreakLocation(spec, "")
			if err != nil {
				return err
			}
			parsed = append(parsed, loc)
		}
		h.pendingBreakpoints = parsed
		return nil
	}
	h.mu.Unlock()
	return run.adapter.SetBreakpoints(locations)
}

// Pause implements session.Debugger. Between scripts it asks the next
// script to stop before its first step.
func (h *scriptDebugHost) Pause() error {
	h.mu.Lock()
	run := h.run
	if run == nil {
		h.pendingPause = true
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	return run.adapter.Pause()
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

	mu      sync.Mutex
	actions []humanActionWire
	// omittedActions counts the steps people ran after actions was full.
	omittedActions int
	stopWatch      context.CancelFunc
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
// ctx. Screenshot files that artifacts writes are offered to the debug
// run, so a step a person runs by hand can name its screenshot. It
// returns nil and a no-op when the session holds no device or another
// script is already being debugged. The returned function must
// run when the script ends.
func (s *DeviceSession) beginScriptDebug(ctx context.Context, orch *orchestra.Orchestra, def *script.ScriptDefinition, artifacts runner.ScriptArtifacts) (*scriptDebugRun, func()) {
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
	if sink, ok := artifacts.(interface{ OnFile(func(string)) }); ok {
		sink.OnFile(run.adapter.NoteArtifact)
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
	id := r.adapter.PauseID()
	if id == 0 {
		return
	}
	r.resumeBudget = suspendToolBudget(r.ctx)
	watchCtx, cancel := context.WithCancel(r.ctx)
	r.stopWatch = cancel
	go r.watchPause(watchCtx, id)
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
// pause id once it has lasted pauseTimeoutMs, until ctx ends.
func (r *scriptDebugRun) watchPause(ctx context.Context, id uint64) {
	var pausedMs int64
	for beat := float64(1); r.timers.Sleep(ctx, pauseProgressIntervalMs) == nil; beat++ {
		pausedMs += pauseProgressIntervalMs
		if pausedMs >= r.pauseTimeoutMs {
			r.autoResume(ctx, id)
			return
		}
		reportPhase(ctx, acquirePhase{step: beat, message: pausedProgressMessage})
	}
}

// resumeIfAbandoned resumes pause id unless a viewer is debugging again
// by now, because that viewer owns the pause.
func (r *scriptDebugRun) resumeIfAbandoned(id uint64) {
	if r.sess.DebugViewers() > 0 {
		return
	}
	r.autoResume(r.ctx, id)
}

// autoResume resumes pause id without the advisory check, because no
// person is there to answer it. It does nothing when the run has left
// that pause, so a run that paused again keeps its new pause.
func (r *scriptDebugRun) autoResume(ctx context.Context, id uint64) {
	_, _ = r.sess.Resume(context.WithoutCancel(ctx), session.ResumeOptions{Force: true, Pause: id})
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

// maxHumanActions caps the steps run_script lists for one call, so a
// long debugging session does not flood the agent's context. The rest are
// counted in the result.
const maxHumanActions = 50

// humanActionWire is a step a person ran on the device while the run was
// paused, as run_script reports it to the agent. Step has the text of a
// type step masked and every secret value redacted. Source and Line say
// where the run was paused, and Line is omitted for a nested step, which
// has none.
type humanActionWire struct {
	Step       string   `json:"step"`
	Command    string   `json:"command"`
	Outcome    string   `json:"outcome"`
	Message    string   `json:"message,omitempty"`
	DurationMs int64    `json:"durationMs"`
	Artifacts  []string `json:"artifacts,omitempty"`
	Source     string   `json:"source,omitempty"`
	Line       int      `json:"line,omitempty"`
}

// humanActionLog is what people did by hand during one call: the first
// maxHumanActions steps in order, and how many came after them.
type humanActionLog struct {
	Entries []humanActionWire
	Omitted int
}

func (r *scriptDebugRun) recordAction(result session.ActResult) {
	loc, _ := r.sess.Paused()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.actions) >= maxHumanActions {
		r.omittedActions++
		return
	}
	r.actions = append(r.actions, humanActionWire{
		Step: result.Step, Command: result.Command, Outcome: result.Outcome,
		Message: result.Message, DurationMs: result.DurationMs, Artifacts: result.Artifacts,
		Source: loc.Source, Line: loc.Line,
	})
}

// humanActions lists, in order, the steps people ran while the run was
// paused. A nil run has none.
func (r *scriptDebugRun) humanActions() humanActionLog {
	if r == nil {
		return humanActionLog{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return humanActionLog{Entries: append([]humanActionWire(nil), r.actions...), Omitted: r.omittedActions}
}
