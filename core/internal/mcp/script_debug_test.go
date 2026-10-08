package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// blockingDriver holds its first screenshot until released, so a test can
// set a breakpoint while a script is mid-run.
type blockingDriver struct {
	*mockdriver.Driver
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	// holdSecond, when set, makes the second screenshot wait until its
	// context ends and close secondEntered when it starts waiting.
	holdSecond    bool
	secondEntered chan struct{}
	calls         atomic.Int32
}

func (b *blockingDriver) Screenshot(ctx context.Context) ([]byte, error) {
	if b.calls.Add(1) == 2 && b.holdSecond {
		close(b.secondEntered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b.once.Do(func() {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
	return b.Driver.Screenshot(ctx)
}

type blockingFactory struct {
	drv  *blockingDriver
	lost chan error
}

func (f blockingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f blockingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
		Lost:      f.lost,
	}, nil
}

// debugScript has its steps on lines 4 and 5.
const debugScript = "appId: com.example.app\nname: Debug\nsteps:\n  - screenshot: first\n  - screenshot: second\n"

type debugHarness struct {
	t        *testing.T
	srv      *Server
	cs       *mcp.ClientSession
	ctx      context.Context
	clk      *clock.ManualClock
	drv      *blockingDriver
	progress chan recordedPhase
	result   chan map[string]any
	viewer   *sessiontest.Viewer
	lost     chan error
}

const defaultDebugPauseMs = 3_600_000

func newDebugHarness(t *testing.T) *debugHarness {
	return newDebugHarnessWithPause(t, defaultDebugPauseMs)
}

// newDebugHarnessWithPause serves the holder's attach socket, so a test
// can connect a real viewer, and bounds a pause at pauseMs.
func newDebugHarnessWithPause(t *testing.T, pauseMs int64) *debugHarness {
	t.Helper()
	leases, err := sessiontest.NewLeases(shortTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	h := &debugHarness{
		t: t, ctx: context.Background(), clk: clock.NewManualClock(0),
		drv:      &blockingDriver{Driver: mockdriver.New(), entered: make(chan struct{}), release: make(chan struct{}), secondEntered: make(chan struct{})},
		progress: make(chan recordedPhase, 32), result: make(chan map[string]any, 1),
		lost: make(chan error, 1),
	}
	h.drv.StageHierarchy(sampleTree())
	h.srv = mustNewServer(t, ServerDeps{
		Probe: device.NewStaticProbe(), Factory: blockingFactory{drv: h.drv, lost: h.lost}, Storage: storage.NewInMemoryStorage(),
		TimerClock: h.clk,
		Session:    session.Deps{Leases: leases, Sockets: leases, Clock: clock.SystemClock{}},
		Timeouts:   session.Timeouts{DebugPauseMs: pauseMs},
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := h.srv.Connect(h.ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "atomyx-test-client", Version: "0.0.0-test"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			h.progress <- recordedPhase{progress: req.Params.Progress, total: req.Params.Total, message: req.Params.Message}
		},
	})
	h.cs, err = client.Connect(h.ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.cs.Close() })
	selectMockDevice(t, h.cs, h.ctx)
	h.viewer = sessiontest.DialViewer(t, leases.SocketPathFor("android", "emulator-5554"), "inspector")
	t.Cleanup(func() {
		if sess := h.held(); sess != nil {
			_, _ = sess.Resume(h.ctx, session.ResumeOptions{Force: true})
		}
		select {
		case <-h.drv.release:
		default:
			close(h.drv.release)
		}
	})
	return h
}

func (h *debugHarness) held() *session.Session {
	h.srv.session.mu.Lock()
	defer h.srv.session.mu.Unlock()
	return h.srv.session.sess
}

// startScript runs script as a run_script call that asks for progress.
func (h *debugHarness) startScript(script string) {
	h.t.Helper()
	h.startCall(map[string]any{"script": script})
}

// startCall runs run_script with args as a call that asks for progress.
func (h *debugHarness) startCall(args map[string]any) {
	h.t.Helper()
	params := &mcp.CallToolParams{Name: "run_script", Arguments: args}
	params.SetProgressToken("debug-run")
	go func() {
		res, err := h.cs.CallTool(h.ctx, params)
		decoded := map[string]any{}
		if err != nil {
			decoded["transportError"] = err.Error()
		} else if text, ok := res.Content[0].(*mcp.TextContent); ok {
			_ = json.Unmarshal([]byte(text.Text), &decoded)
		}
		h.result <- decoded
	}()
}

// pauseBeforeLine starts script, sets an inline breakpoint on line while the
// first step is held in the driver, and returns once the run is paused.
func (h *debugHarness) pauseBeforeLine(script string, line string) session.PauseLocation {
	h.t.Helper()
	return h.pauseBeforeLineOf(map[string]any{"script": script}, line)
}

// pauseBeforeLineOf is pauseBeforeLine for a run_script call with args.
func (h *debugHarness) pauseBeforeLineOf(args map[string]any, line string) session.PauseLocation {
	h.t.Helper()
	h.startCall(args)
	h.awaitEntered()
	if err := h.viewer.SetBreakpoints([]string{"inline:" + line}); err != nil {
		h.t.Fatalf("SetBreakpoints: %v", err)
	}
	close(h.drv.release)
	var loc session.PauseLocation
	eventually(h.t, "the run to pause", func() bool {
		var paused bool
		loc, paused = h.held().Paused()
		return paused
	})
	return loc
}

func (h *debugHarness) awaitEntered() {
	h.t.Helper()
	select {
	case <-h.drv.entered:
	case <-time.After(yieldWait):
		h.t.Fatal("the script never reached its first step")
	}
}

func (h *debugHarness) awaitResult() map[string]any {
	h.t.Helper()
	select {
	case r := <-h.result:
		return r
	case <-time.After(yieldWait):
		h.t.Fatal("run_script did not return")
		return nil
	}
}

func TestScriptDebug_AnInlineScriptPausesAtItsInlineLocation(t *testing.T) {
	h := newDebugHarness(t)
	loc := h.pauseBeforeLine(debugScript, "5")
	if loc.Source != "inline" || loc.Line != 5 || loc.Command != "screenshot" || loc.Depth != 0 {
		t.Fatalf("paused at %+v, want inline line 5 before the screenshot", loc)
	}
	if got := h.held().Info().State; got != session.StatePaused {
		t.Fatalf("state = %q, want paused", got)
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result after resume = %v, want ok", got)
	}
}

func TestScriptDebug_ABreakpointThatMatchesNoStepIsRefusedAndBetweenScriptsNothingIsRunning(t *testing.T) {
	h := newDebugHarness(t)
	if got := codeOfErr(h.held().SetBreakpoints([]string{"inline:5"})); got != diag.CodeHolderBusy {
		t.Fatalf("breakpoints between scripts: %q, want %s", got, diag.CodeHolderBusy)
	}
	h.startScript(debugScript)
	h.awaitEntered()
	if got := codeOfErr(h.held().SetBreakpoints([]string{"inline:9"})); got != diag.CodeInvalidArgument {
		t.Fatalf("breakpoint on a line with no step: %q, want %s", got, diag.CodeInvalidArgument)
	}
	close(h.drv.release)
	h.awaitResult()
}

func TestScriptDebug_APausedCallDoesNotTimeOutAndKeepsReportingProgress(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")
	h.clk.AwaitSleepers(1)

	h.clk.Advance(pauseProgressIntervalMs)
	select {
	case p := <-h.progress:
		if p.message != pausedProgressMessage || p.progress != 1 || p.total != 0 {
			t.Fatalf("progress = %+v, want beat 1 of an unknown total with the paused message", p)
		}
	case <-time.After(yieldWait):
		t.Fatal("no progress while paused")
	}

	const farPastAnyToolBudgetMs = 3_600_000
	h.clk.Advance(farPastAnyToolBudgetMs)
	select {
	case r := <-h.result:
		t.Fatalf("the paused call returned: %v", r)
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result = %v, want the script to pass instead of timing out", got)
	}
}

func TestScriptDebug_APausedSessionNeverYieldsToAnotherProcess(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	_, err := h.srv.session.Yield(h.ctx, "atomyx run")
	if got := codeOfErr(err); got != diag.CodeHolderBusy {
		t.Fatalf("yield while paused = %q (%v), want %s", got, err, diag.CodeHolderBusy)
	}
	if h.held() == nil {
		t.Fatal("the session gave its device up while paused")
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	h.awaitResult()
}

func TestScriptDebug_TheRunResumesWhenTheLastDebuggingViewerLeaves(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	if err := h.viewer.Close(); err != nil {
		t.Fatal(err)
	}
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result = %v, want the run to finish once its only debugging viewer left", got)
	}
}

func TestScriptDebug_ThePauseTimeoutResumesTheRun(t *testing.T) {
	const pauseMs = 3 * pauseProgressIntervalMs
	h := newDebugHarnessWithPause(t, pauseMs)
	h.pauseBeforeLine(debugScript, "5")

	for range 3 {
		h.clk.AwaitSleepers(1)
		h.clk.Advance(pauseProgressIntervalMs)
	}
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result = %v, want the run to resume after the pause timeout", got)
	}
}

func TestScriptDebug_DisconnectingTheDeviceWhilePausedEndsTheRunAsDeviceLost(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	if _, decoded := callTool(t, h.cs, h.ctx, "disconnect_device", map[string]any{}); decoded["ok"] == false {
		t.Fatalf("disconnect_device = %v", decoded)
	}
	got := h.awaitResult()
	abort := requireDeviceLostAbort(t, got, diag.CodeDeviceNotFound, deviceDisconnectedPausedMessage)
	if remedy, _ := abort["remedy"].(string); !strings.Contains(remedy, "select_device") {
		t.Fatalf("remedy = %q, want the agent told to select a device again", remedy)
	}
	requireStepsKept(t, got, 1, 2)
}

func TestScriptDebug_ADeviceLostWhilePausedEndsTheRunWithItsStepsAndTheCodeOfTheLoss(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	h.lost <- &device.ProcessExitError{Component: device.LostRunner}
	got := h.awaitResult()
	requireDeviceLostAbort(t, got, diag.CodeAgentUnreachable, deviceLostWhilePausedMessage)
	requireStepsKept(t, got, 1, 2)
}

func TestScriptDebug_ADeviceLostMidStepDoesNotClaimTheRunWasPaused(t *testing.T) {
	h := newDebugHarness(t)
	h.startScript(debugScript)
	h.awaitEntered()

	h.lost <- &device.ProcessExitError{Component: device.LostRunner}
	got := h.awaitResult()
	requireDeviceLostAbort(t, got, diag.CodeAgentUnreachable, deviceLostMessage)
	requireStepsKept(t, got, 1, 2)
}

func TestScriptDebug_DisconnectingTheDeviceMidStepEndsTheRunWithoutAPause(t *testing.T) {
	h := newDebugHarness(t)
	h.startScript(debugScript)
	h.awaitEntered()

	if _, decoded := callTool(t, h.cs, h.ctx, "disconnect_device", map[string]any{}); decoded["ok"] == false {
		t.Fatalf("disconnect_device = %v", decoded)
	}
	got := h.awaitResult()
	requireDeviceLostAbort(t, got, diag.CodeDeviceNotFound, deviceDisconnectedMessage)
	requireStepsKept(t, got, 1, 2)
}

// requireDeviceLostAbort checks that got is a failed run that stopped as
// device lost with the given code and message, and returns the abort.
func requireDeviceLostAbort(t *testing.T, got map[string]any, code, message string) map[string]any {
	t.Helper()
	abort, _ := got["abort"].(map[string]any)
	if got["ok"] != false || got["deviceLost"] != true || abort["reason"] != "deviceLost" {
		t.Fatalf("result = %v, want a run that ended as device lost", got)
	}
	if abort["code"] != code || abort["message"] != message {
		t.Fatalf("abort = %v, want code %s and message %q", abort, code, message)
	}
	return abort
}

// requireStepsKept checks that the result still reports the steps that
// ran and the size of the script.
func requireStepsKept(t *testing.T, got map[string]any, ran, total int) {
	t.Helper()
	steps, _ := got["steps"].([]any)
	if len(steps) != ran || got["totalSteps"] != float64(total) {
		t.Fatalf("steps = %v totalSteps = %v, want %d steps of %d kept", got["steps"], got["totalSteps"], ran, total)
	}
}

func TestScriptDebug_TheAgentSeesEachHumanActionWithTypedTextMasked(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	for _, step := range []string{"screenshot: by-hand", "type: hunter2hunter2"} {
		if _, err := h.held().Act(h.ctx, step); err != nil {
			t.Fatalf("Act(%q): %v", step, err)
		}
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	got := h.awaitResult()

	actions, _ := got["humanActions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("humanActions = %v, want the two steps in order", got["humanActions"])
	}
	first, _ := actions[0].(map[string]any)
	second, _ := actions[1].(map[string]any)
	if first["command"] != "screenshot" || first["outcome"] != "passed" || second["command"] != "type" {
		t.Fatalf("humanActions = %v", actions)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "hunter2hunter2") {
		t.Fatalf("the result leaks the typed text: %s", encoded)
	}
}

func TestScriptDebug_AScriptNobodyDebuggedCarriesNoHumanActions(t *testing.T) {
	h := newDebugHarness(t)
	h.startScript(debugScript)
	h.awaitEntered()
	close(h.drv.release)
	got := h.awaitResult()
	if _, present := got["humanActions"]; present || got["ok"] != true {
		t.Fatalf("result = %v, want ok and no humanActions field", got)
	}
}

// threeStepScript has its steps on lines 4, 5 and 6.
const threeStepScript = "appId: com.example.app\nname: Debug\nsteps:\n  - screenshot: first\n  - screenshot: second\n  - screenshot: third\n"

func (h *debugHarness) debugRun() *scriptDebugRun {
	h.t.Helper()
	run := h.srv.session.debug.current()
	if run == nil {
		h.t.Fatal("no script is being debugged")
	}
	return run
}

func TestScriptDebug_WhatAViewerAskedForEndsWithTheViewer(t *testing.T) {
	h := newDebugHarness(t)
	h.startScript(debugScript)
	h.awaitEntered()
	if err := h.viewer.SetBreakpoints([]string{"inline:5"}); err != nil {
		t.Fatal(err)
	}
	run := h.debugRun()
	if got := run.adapter.Unhit(); len(got) != 1 {
		t.Fatalf("breakpoints = %v, want the one the viewer set", got)
	}

	if err := h.viewer.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the viewer's breakpoints to be dropped", func() bool { return len(run.adapter.Unhit()) == 0 })

	close(h.drv.release)
	if got := h.awaitResult(); got["ok"] != true {
		t.Fatalf("result = %v, want the run to pass the old breakpoint line without pausing", got)
	}
}

func TestScriptDebug_ARunAViewerStillDebugsIsNotResumedOnItsBehalf(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")
	run := h.debugRun()

	run.resumeIfAbandoned(run.adapter.PauseID())

	if _, paused := h.held().Paused(); !paused {
		t.Fatal("the run was resumed while a viewer was debugging it")
	}
}

func TestScriptDebug_AnAutoResumeAimedAtAnEarlierPauseLeavesTheNextPauseAlone(t *testing.T) {
	h := newDebugHarness(t)
	h.startScript(threeStepScript)
	h.awaitEntered()
	if err := h.viewer.SetBreakpoints([]string{"inline:5", "inline:6"}); err != nil {
		t.Fatal(err)
	}
	close(h.drv.release)
	awaitPausedAtLine := func(line int) {
		eventually(t, "the run to pause", func() bool {
			loc, paused := h.held().Paused()
			return paused && loc.Line == line
		})
	}
	awaitPausedAtLine(5)
	run := h.debugRun()
	firstPause := run.adapter.PauseID()
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	awaitPausedAtLine(6)

	run.autoResume(h.ctx, firstPause)

	if loc, paused := h.held().Paused(); !paused || loc.Line != 6 {
		t.Fatalf("paused = %v at %+v, want the second pause untouched by the timeout of the first", paused, loc)
	}
}

func TestScriptDebug_AScreenshotAPersonTookIsNamedInTheActionAndTheIntervention(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")

	acted, err := h.held().Act(h.ctx, "screenshot: by-hand")
	if err != nil {
		t.Fatal(err)
	}
	if len(acted.Artifacts) != 1 || !strings.HasPrefix(acted.Artifacts[0], "artifacts/script-") {
		t.Fatalf("act artifacts = %v, want the screenshot file the act wrote", acted.Artifacts)
	}
	var recorded []string
	for _, entry := range h.held().Interventions() {
		recorded = append(recorded, entry.Artifacts...)
	}
	if len(recorded) != 1 || recorded[0] != acted.Artifacts[0] {
		t.Fatalf("intervention artifacts = %v, want %v", recorded, acted.Artifacts)
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	actions, _ := h.awaitResult()["humanActions"].([]any)
	first, _ := actions[0].(map[string]any)
	if got, _ := first["artifacts"].([]any); len(got) != 1 || got[0] != acted.Artifacts[0] {
		t.Fatalf("humanActions[0] = %v, want the screenshot %s", first, acted.Artifacts[0])
	}
}

func TestScriptDebug_ACallKeepsWhatWasLeftOfItsBudgetAcrossAPause(t *testing.T) {
	const (
		beforePauseMs = 10_000
		pausedMs      = 5_000
	)
	h := newDebugHarness(t)
	h.drv.holdSecond = true
	t.Cleanup(func() { h.clk.Advance(2 * defaultToolTimeoutMs) })
	h.startScript(debugScript)
	h.awaitEntered()
	h.clk.AwaitSleepers(1)
	h.clk.Advance(beforePauseMs)
	if err := h.viewer.SetBreakpoints([]string{"inline:5"}); err != nil {
		t.Fatal(err)
	}
	close(h.drv.release)
	eventually(t, "the run to pause", func() bool { _, paused := h.held().Paused(); return paused })

	h.clk.AwaitSleepers(1)
	h.clk.Advance(pausedMs)
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.drv.secondEntered:
	case <-time.After(yieldWait):
		t.Fatal("the second step never started")
	}
	h.clk.AwaitSleepers(1)

	h.clk.Advance(defaultToolTimeoutMs - beforePauseMs - 1)
	select {
	case r := <-h.result:
		t.Fatalf("the call ended with budget left: %v", r)
	case <-time.After(50 * time.Millisecond):
	}
	h.clk.Advance(1)
	if got := h.awaitResult(); got["code"] != diag.CodeToolTimeout {
		t.Fatalf("result = %v, want %s once the part of the budget left after the pause was spent", got, diag.CodeToolTimeout)
	}
}

func TestScriptDebug_EachHumanActionSaysWhereTheRunWasPaused(t *testing.T) {
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")
	if _, err := h.held().Act(h.ctx, "screenshot: by-hand"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	actions, _ := h.awaitResult()["humanActions"].([]any)
	first, _ := actions[0].(map[string]any)
	if first["source"] != "inline" || first["line"] != float64(5) {
		t.Fatalf("humanActions[0] = %v, want the pause at inline line 5", first)
	}
}

func TestScriptDebug_TheListOfHumanActionsIsCappedAndTheRestCounted(t *testing.T) {
	const extra = 3
	h := newDebugHarness(t)
	h.pauseBeforeLine(debugScript, "5")
	for range maxHumanActions + extra {
		if _, err := h.held().Act(h.ctx, "screenshot: by-hand"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	got := h.awaitResult()
	if actions, _ := got["humanActions"].([]any); len(actions) != maxHumanActions {
		t.Fatalf("humanActions has %d entries, want the cap of %d", len(actions), maxHumanActions)
	}
	if got["humanActionsOmittedCount"] != float64(extra) {
		t.Fatalf("humanActionsOmittedCount = %v, want %d", got["humanActionsOmittedCount"], extra)
	}
}

func TestScriptDebug_ASecretInAFailedHumanActionIsMasked(t *testing.T) {
	const secret = "hunter2hunter2"
	h := newDebugHarness(t)
	h.pauseBeforeLineOf(map[string]any{"script": debugScript, "env": map[string]string{"SECRET": secret}}, "5")
	acted, err := h.held().Act(h.ctx, "tap: ${SECRET}")
	if err != nil {
		t.Fatal(err)
	}
	if acted.Outcome != "failed" {
		t.Fatalf("act outcome = %q, want the tap to fail", acted.Outcome)
	}
	if _, err := h.held().Resume(h.ctx, session.ResumeOptions{}); err != nil {
		t.Fatal(err)
	}
	got := h.awaitResult()
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("the result leaks the secret: %s", encoded)
	}
	actions, _ := got["humanActions"].([]any)
	first, _ := actions[0].(map[string]any)
	if first["outcome"] != "failed" || first["message"] != orchestra.SecureMask {
		t.Fatalf("humanActions[0] = %v, want a failed action with its message masked", first)
	}
}
