package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

func TestScript_RunScript_MissingYaml_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterScriptFeature(ctx)

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", json.RawMessage(`{}`))
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestScript_RunScript_NoDeviceSelected_ReturnsNoDeviceSelectedMessage(t *testing.T) {
	ctx := newTestContext()
	RegisterScriptFeature(ctx)

	params, _ := json.Marshal(map[string]string{"yaml": "appId: com.test\n---\n- back\n"})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)
	if err == nil || err.Error() != "No device selected" {
		t.Fatalf("Dispatch() error = %v, want %q", err, "No device selected")
	}
}

func TestScript_IsRunning_FalseWhenIdle(t *testing.T) {
	ctx := newTestContext()
	RegisterScriptFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "isRunning", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got, ok := result.(isRunningResult); !ok || got.Running {
		t.Fatalf("Dispatch() result = %#v, want {Running:false}", result)
	}
}

func TestScript_StopScript_WhenIdle_IsNoOp(t *testing.T) {
	ctx := newTestContext()
	RegisterScriptFeature(ctx)

	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "stopScript", nil); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
}

func TestScript_RunScript_EmitsEventSequenceAndReturnsResult(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	RegisterScriptFeature(ctx)

	var mu sync.Mutex
	var eventNames []string
	unsubscribe := ctx.Events.Subscribe(func(e Event) {
		mu.Lock()
		eventNames = append(eventNames, e.Event)
		mu.Unlock()
	})
	defer unsubscribe()

	yaml := "appId: com.test\n---\n- launchApp\n- screenshot: first\n"
	params, _ := json.Marshal(map[string]string{"yaml": yaml})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	got, ok := result.(runScriptResultWire)
	if !ok {
		t.Fatalf("Dispatch() result = %#v, want runScriptResultWire", result)
	}
	if got.RunID == "" {
		t.Fatal("RunID is empty")
	}
	if !got.Result.Ok || got.Result.TotalSteps != 2 || got.Result.PassedSteps != 2 {
		t.Fatalf("Result = %+v, want Ok=true TotalSteps=2 PassedSteps=2", got.Result)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"runStarted", "stepStarted", "stepCompleted", "stepStarted", "stepCompleted", "runCompleted"}
	if len(eventNames) != len(want) {
		t.Fatalf("events = %v, want %v", eventNames, want)
	}
	for i := range want {
		if eventNames[i] != want[i] {
			t.Fatalf("events = %v, want %v", eventNames, want)
		}
	}
}

// TestScript_RunScript_InvalidYaml_DoesNotLeakRunState pins the
// no-leak contract for a script that fails to parse: the run must
// never be recorded as in flight, so isRunning reports false right
// after the failure and a subsequent valid run is accepted rather
// than rejected as already in progress. The cross-engine differential
// case for this scenario belongs with the sidecar differential
// harness once it exists; today only the TypeScript and Go sidecars
// are checked independently.
func TestScript_RunScript_InvalidYaml_DoesNotLeakRunState(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterScriptFeature(ctx)

	invalidParams, _ := json.Marshal(map[string]string{"yaml": "- bogusCommand\n"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", invalidParams); err == nil {
		t.Fatal("Dispatch() error = nil, want a parse error for an invalid script")
	}

	running, err := ctx.Dispatcher.Dispatch(context.Background(), "isRunning", nil)
	if err != nil {
		t.Fatalf("isRunning: Dispatch() error = %v", err)
	}
	if running.(isRunningResult).Running {
		t.Fatal("isRunning = true after a parse failure, want false")
	}

	validParams, _ := json.Marshal(map[string]string{"yaml": "appId: com.test\n---\n- back\n"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", validParams); err != nil {
		t.Fatalf("second run: Dispatch() error = %v, want the run to be accepted", err)
	}
}

func TestScript_RunScript_ConcurrentRun_Rejected(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterScriptFeature(ctx)

	yaml := "appId: com.test\n---\n- sleep: 150\n"
	params, _ := json.Marshal(map[string]string{"yaml": yaml})

	started := make(chan struct{})
	var once sync.Once
	unsubscribe := ctx.Events.Subscribe(func(e Event) {
		if e.Event == "runStarted" {
			once.Do(func() { close(started) })
		}
	})
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params); err != nil {
			t.Errorf("first run: Dispatch() error = %v", err)
		}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start in time")
	}

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)
	if err == nil || err.Error() != "A run is already in progress" {
		t.Fatalf("second run: Dispatch() error = %v, want %q", err, "A run is already in progress")
	}

	running, err := ctx.Dispatcher.Dispatch(context.Background(), "isRunning", nil)
	if err != nil {
		t.Fatalf("isRunning: Dispatch() error = %v", err)
	}
	if !running.(isRunningResult).Running {
		t.Fatal("isRunning = false while the first run is in flight")
	}

	<-done

	running, err = ctx.Dispatcher.Dispatch(context.Background(), "isRunning", nil)
	if err != nil {
		t.Fatalf("isRunning: Dispatch() error = %v", err)
	}
	if running.(isRunningResult).Running {
		t.Fatal("isRunning = true after the run finished")
	}
}

func TestScript_StopScript_CancelsInFlightRun(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterScriptFeature(ctx)

	yaml := "appId: com.test\n---\n- sleep: 5000\n"
	params, _ := json.Marshal(map[string]string{"yaml": yaml})

	started := make(chan struct{})
	var once sync.Once
	unsubscribe := ctx.Events.Subscribe(func(e Event) {
		if e.Event == "runStarted" {
			once.Do(func() { close(started) })
		}
	})
	defer unsubscribe()

	type outcome struct {
		result any
		err    error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		result, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)
		resultCh <- outcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not start in time")
	}

	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "stopScript", nil); err != nil {
		t.Fatalf("stopScript: Dispatch() error = %v", err)
	}

	select {
	case out := <-resultCh:
		// Mid-step cancellation is caught by the sleep command's own
		// exception handling, so it normally surfaces as a completed
		// run with Ok=false rather than a Go error out of Dispatch; a
		// non-nil error is also an acceptable stopped-early outcome.
		if out.err == nil {
			got, ok := out.result.(runScriptResultWire)
			if !ok || got.Result.Ok {
				t.Fatalf("Dispatch() result = %#v, want a failed result after stopScript", out.result)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop within 2s of stopScript")
	}
}
