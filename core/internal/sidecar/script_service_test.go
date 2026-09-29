package sidecar

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

// unreachableAgentError models a driver liveness failure the same way
// a real driver's coded error does (an Error, a Code, and a Remedy),
// without this test depending on the diagnostics package: the health
// check recognizes any error matching that method set, structurally,
// regardless of which package defines it.
type unreachableAgentError struct {
	msg    string
	remedy string
}

func (e unreachableAgentError) Error() string  { return e.msg }
func (e unreachableAgentError) Code() string   { return "AGENT_UNREACHABLE" }
func (e unreachableAgentError) Remedy() string { return e.remedy }

// TestScriptService_Run_ConcurrentCalls_SecondIsRejectedBeforeItParses
// guards against the specific race two concurrent Run calls can
// otherwise hit: the "one run at a time" guard and the write that
// claims the slot were two separate lock acquisitions, with parsing
// running unlocked in between, so a second call arriving in that
// window could pass the guard too and start its own parse instead of
// being rejected.
func TestScriptService_Run_ConcurrentCalls_SecondIsRejectedBeforeItParses(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	svc := newScriptService(ctx.Session, ctx.Events)

	firstEnteredParse := make(chan struct{})
	var enteredOnce sync.Once
	releaseParse := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseParse) }) })

	var parseCalls int32
	svc.parseScript = func(yamlContent string, externalEnv map[string]string) (*script.ScriptDefinition, error) {
		atomic.AddInt32(&parseCalls, 1)
		enteredOnce.Do(func() { close(firstEnteredParse) })
		<-releaseParse
		return script.ParseScript(yamlContent, externalEnv)
	}

	yaml := "appId: com.test\n---\n- launchApp\n"

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.Run(context.Background(), yaml)
		firstDone <- err
	}()

	select {
	case <-firstEnteredParse:
	case <-time.After(2 * time.Second):
		t.Fatal("first Run() never reached parseScript")
	}

	// The run slot must already be reserved before the first call's
	// parse finishes, so a second call arriving here is rejected
	// immediately instead of racing into its own parse.
	secondDone := make(chan error, 1)
	go func() {
		_, err := svc.Run(context.Background(), yaml)
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		if !errors.Is(err, errRunAlreadyInProgress) {
			t.Fatalf("second Run() error = %v, want errRunAlreadyInProgress", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second Run() did not return promptly: it appears to have entered parseScript concurrently with the first, meaning the run slot was not reserved before parsing")
	}

	releaseOnce.Do(func() { close(releaseParse) })
	if err := <-firstDone; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	if got := atomic.LoadInt32(&parseCalls); got != 1 {
		t.Fatalf("parseScript was called %d times, want exactly 1 (the second call must never reach parseScript while the first holds the run slot)", got)
	}
}

// TestScriptService_RunScenario_WhenRunSlotReserved_ReturnsAlreadyInProgress
// confirms RunScenario is guarded by the same atomic reservation as
// Run, not a separate check-then-set pair of its own.
func TestScriptService_RunScenario_WhenRunSlotReserved_ReturnsAlreadyInProgress(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	svc := newScriptService(ctx.Session, ctx.Events)

	if err := svc.reserve(); err != nil {
		t.Fatalf("reserve() error = %v", err)
	}

	_, err := svc.RunScenario(context.Background(), "name: x\nscripts: []\n", ".")
	if !errors.Is(err, errRunAlreadyInProgress) {
		t.Fatalf("RunScenario() error = %v, want errRunAlreadyInProgress", err)
	}
}

// TestScriptService_Run_ParseFailure_ReleasesTheReservedSlot confirms
// the placeholder reservation Run installs before parsing is released
// on a parse failure, so a subsequent call is accepted rather than
// permanently rejected.
func TestScriptService_Run_ParseFailure_ReleasesTheReservedSlot(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	svc := newScriptService(ctx.Session, ctx.Events)

	if _, err := svc.Run(context.Background(), "not valid yaml: [\n"); err == nil {
		t.Fatal("Run() error = nil, want a parse error for invalid yaml")
	}
	if svc.IsRunning() {
		t.Fatal("IsRunning() = true after a parse failure, want the reservation released")
	}

	yaml := "appId: com.test\n---\n- launchApp\n"
	if _, err := svc.Run(context.Background(), yaml); err != nil {
		t.Fatalf("Run() after a prior parse failure: error = %v, want the slot to have been released", err)
	}
}

// TestScriptService_Run_DeviceLostAbort_CarriedIntoWireResult guards
// against a genuine device-lost abort being computed by the runner
// but dropped before it reaches the wire: it fails a step against a
// live (uncanceled) context, with the mock driver's Ping staged to
// report the agent unreachable, and asserts the resulting
// runScriptResultWire carries the same abort reason, code, message,
// and remedy runner.ScriptResult.Abort produced, not just an internal
// struct field a caller of Run never sees.
func TestScriptService_Run_DeviceLostAbort_CarriedIntoWireResult(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	drv.FailNextPingForContract(unreachableAgentError{
		msg:    "could not reach the agent on device mock-1",
		remedy: "Confirm the agent is running on the device, then try again.",
	})

	svc := newScriptService(ctx.Session, ctx.Events)

	yaml := "appId: com.test\n---\n- assertVisible: \"Does not exist\"\n"
	result, err := svc.Run(context.Background(), yaml)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Result.Abort == nil {
		t.Fatal("Result.Abort is nil, want the device-lost abort surfaced on the wire")
	}
	if result.Result.Abort.Reason != string(runner.RunAbortDeviceLost) {
		t.Fatalf("Result.Abort.Reason = %q, want %q", result.Result.Abort.Reason, runner.RunAbortDeviceLost)
	}
	if result.Result.Abort.Code != "AGENT_UNREACHABLE" {
		t.Fatalf("Result.Abort.Code = %q, want %q", result.Result.Abort.Code, "AGENT_UNREACHABLE")
	}
	if result.Result.Abort.Message == "" {
		t.Fatal("Result.Abort.Message is empty, want the probe's diagnostic message")
	}
	if result.Result.Abort.Remedy == "" {
		t.Fatal("Result.Abort.Remedy is empty, want an actionable hint")
	}
}
