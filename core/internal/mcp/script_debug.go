package mcp

import (
	"context"
	"sync"

	"dev.atomyx/internal/debugrun"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
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
// held session paused.
type scriptDebugRun struct {
	adapter *debugrun.Adapter
	sess    *session.Session
}

var (
	_ runner.StepGate = (*scriptDebugRun)(nil)
	_ debugrun.Pauser = (*scriptDebugRun)(nil)
)

// beginScriptDebug starts debugging the script def, run by orch under
// ctx. It returns nil and a no-op when the session holds no device or
// another script is already being debugged. The returned function must
// run when the script ends.
func (s *DeviceSession) beginScriptDebug(_ context.Context, orch *orchestra.Orchestra, def *script.ScriptDefinition) (*scriptDebugRun, func()) {
	s.mu.Lock()
	held := s.sess
	s.mu.Unlock()
	if held == nil {
		return nil, func() {}
	}
	run := &scriptDebugRun{sess: held}
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

// BeforeStep implements runner.StepGate.
func (r *scriptDebugRun) BeforeStep(ctx context.Context, step runner.GateStep, run runner.PausedRun) error {
	return r.adapter.BeforeStep(ctx, step, run)
}

// EnterPause implements debugrun.Pauser.
func (r *scriptDebugRun) EnterPause(loc session.PauseLocation) {
	r.sess.EnterPause(loc)
}

// LeavePause implements debugrun.Pauser.
func (r *scriptDebugRun) LeavePause() {
	r.sess.LeavePause()
}

// Closed implements debugrun.Pauser.
func (r *scriptDebugRun) Closed() <-chan struct{} { return r.sess.Closed() }

// Lost implements debugrun.Pauser.
func (r *scriptDebugRun) Lost() <-chan struct{} { return r.sess.Lost() }
