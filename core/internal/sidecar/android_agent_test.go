package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/device"
)

func subscribeAndroidAgentEvents(ctx *Context) (statuses func() []AndroidAgentStatus, unsubscribe func()) {
	var mu sync.Mutex
	var seen []AndroidAgentStatus
	unsub := ctx.Events.Subscribe(func(e Event) {
		if e.Event != "androidAgentStatus" {
			return
		}
		mu.Lock()
		seen = append(seen, e.Payload.(AndroidAgentStatus))
		mu.Unlock()
	})
	return func() []AndroidAgentStatus {
		mu.Lock()
		defer mu.Unlock()
		return append([]AndroidAgentStatus(nil), seen...)
	}, unsub
}

func waitForAndroidAgentState(t *testing.T, statuses func() []AndroidAgentStatus, want AndroidAgentState) AndroidAgentStatus {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, s := range statuses() {
			if s.State == want {
				return s
			}
		}
		select {
		case <-deadline:
			t.Fatalf("state %q not observed within timeout", want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestAndroidAgent_Status_UnknownSerial_ReturnsIdle(t *testing.T) {
	ctx := newTestContext()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(device.NewFakeProvisioner(), ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "androidAgentStatus", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(AndroidAgentStatus)
	want := AndroidAgentStatus{Serial: "serial-1", State: AndroidAgentStateIdle, Port: 8765}
	if !ok || got != want {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_MissingSerial_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(device.NewFakeProvisioner(), ctx.Events, nil))

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", json.RawMessage(`{}`))
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_ReturnsInstallingImmediately(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	gate := make(chan struct{})
	provisioner.SetGate(gate)
	defer close(gate)
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(AndroidAgentStatus)
	want := AndroidAgentStatus{Serial: "serial-1", State: AndroidAgentStateInstalling, Port: 8765}
	if !ok || got != want {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_Success_TransitionsToReadyAndEmits(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	statuses, unsubscribe := subscribeAndroidAgentEvents(ctx)
	defer unsubscribe()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	ready := waitForAndroidAgentState(t, statuses, AndroidAgentStateReady)
	want := AndroidAgentStatus{Serial: "serial-1", State: AndroidAgentStateReady, Port: 8765}
	if ready != want {
		t.Fatalf("ready status = %#v, want %#v", ready, want)
	}

	statusResult, err := ctx.Dispatcher.Dispatch(context.Background(), "androidAgentStatus", params)
	if err != nil {
		t.Fatalf("androidAgentStatus: Dispatch() error = %v", err)
	}
	if statusResult.(AndroidAgentStatus) != want {
		t.Fatalf("androidAgentStatus result = %#v, want %#v", statusResult, want)
	}

	calls := provisioner.AndroidCalls()
	if len(calls) != 1 || calls[0] != "serial-1" {
		t.Fatalf("AndroidCalls() = %#v, want one call for serial-1", calls)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_Failure_TransitionsToFailedWithMessage(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	provisioner.SetAndroidResult("serial-1", errors.New("boom"))
	statuses, unsubscribe := subscribeAndroidAgentEvents(ctx)
	defer unsubscribe()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	failed := waitForAndroidAgentState(t, statuses, AndroidAgentStateFailed)
	want := AndroidAgentStatus{Serial: "serial-1", State: AndroidAgentStateFailed, Message: "boom", Port: 8765}
	if failed != want {
		t.Fatalf("failed status = %#v, want %#v", failed, want)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_WhileInstalling_DedupsWithoutRestarting(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	gate := make(chan struct{})
	provisioner.SetGate(gate)
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	first, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params)
	if err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	waitForAndroidCallCount(t, provisioner, 1)

	second, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params)
	if err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	if first.(AndroidAgentStatus) != second.(AndroidAgentStatus) {
		t.Fatalf("first = %#v, second = %#v, want equal installing statuses", first, second)
	}
	close(gate)

	if calls := provisioner.AndroidCalls(); len(calls) != 1 {
		t.Fatalf("AndroidCalls() = %#v, want exactly one call while installing", calls)
	}
}

// panickingProvisioner always panics instead of returning from either
// method, standing in for a provisioner call that crashes partway
// through (for example on an unexpected adb or XCUITest response
// shape). Shared by the Android and iOS agent panic-recovery tests.
type panickingProvisioner struct{}

var _ device.AgentProvisioner = panickingProvisioner{}

func (panickingProvisioner) EnsureIOSAgent(context.Context, string, device.IosAgentKind) (int, func() error, error) {
	panic("ios agent panic")
}

func (panickingProvisioner) EnsureIOSAgentWatched(context.Context, string, device.IosAgentKind, func(error)) (int, func() error, error) {
	panic("ios agent panic")
}

func (panickingProvisioner) EnsureAndroidAgent(context.Context, string) error {
	panic("android agent panic")
}

// TestAndroidAgent_EnsureAndroidAgent_ProvisionerPanics_RecoversAndMarksFailed
// guards against a background provisioning panic crashing the whole
// process: the goroutine must recover, report through the injected
// PanicHandler, and mark the entry failed so a caller sees a failure
// instead of a silent hang.
func TestAndroidAgent_EnsureAndroidAgent_ProvisionerPanics_RecoversAndMarksFailed(t *testing.T) {
	ctx := newTestContext()
	statuses, unsubscribe := subscribeAndroidAgentEvents(ctx)
	defer unsubscribe()

	var gotSource string
	var gotRecovered any
	var gotStack []byte
	onPanic := func(source string, recovered any, stack []byte) {
		gotSource = source
		gotRecovered = recovered
		gotStack = stack
	}
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(panickingProvisioner{}, ctx.Events, onPanic))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	failed := waitForAndroidAgentState(t, statuses, AndroidAgentStateFailed)
	if failed.Serial != "serial-1" || failed.Message == "" {
		t.Fatalf("failed status = %#v, want serial-1 with a non-empty panic message", failed)
	}

	// onPanic and markFailed both run, in that order, on the same
	// background goroutine before markFailed's event publish. The
	// waitForAndroidAgentState call above already observed that
	// publish, so its side effects, including onPanic's, are visible
	// here without further synchronization.
	if gotSource != "AndroidAgentService.Ensure" {
		t.Fatalf("onPanic source = %q, want %q", gotSource, "AndroidAgentService.Ensure")
	}
	if gotRecovered != "android agent panic" {
		t.Fatalf("onPanic recovered = %v, want %q", gotRecovered, "android agent panic")
	}
	if len(gotStack) == 0 {
		t.Fatal("onPanic stack = empty, want a captured stack trace")
	}
}

func waitForAndroidCallCount(t *testing.T, provisioner *device.FakeProvisioner, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if len(provisioner.AndroidCalls()) >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("AndroidCalls() did not reach %d within timeout", want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestAndroidAgent_EnsureAndroidAgent_WhileReady_DedupsWithoutRestarting(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	statuses, unsubscribe := subscribeAndroidAgentEvents(ctx)
	defer unsubscribe()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForAndroidAgentState(t, statuses, AndroidAgentStateReady)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params)
	if err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	want := AndroidAgentStatus{Serial: "serial-1", State: AndroidAgentStateReady, Port: 8765}
	if result.(AndroidAgentStatus) != want {
		t.Fatalf("second ensure result = %#v, want %#v", result, want)
	}
	if calls := provisioner.AndroidCalls(); len(calls) != 1 {
		t.Fatalf("AndroidCalls() = %#v, want exactly one call once ready", calls)
	}
}

func TestAndroidAgent_EnsureAndroidAgent_AfterFailure_Retries(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	provisioner.SetAndroidResult("serial-1", errors.New("boom"))
	statuses, unsubscribe := subscribeAndroidAgentEvents(ctx)
	defer unsubscribe()
	RegisterAndroidAgentFeature(ctx, NewAndroidAgentService(provisioner, ctx.Events, nil))

	params, _ := json.Marshal(map[string]string{"serial": "serial-1"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForAndroidAgentState(t, statuses, AndroidAgentStateFailed)

	provisioner.SetAndroidResult("serial-1", nil)
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureAndroidAgent", params); err != nil {
		t.Fatalf("retry Dispatch() error = %v", err)
	}
	waitForAndroidAgentState(t, statuses, AndroidAgentStateReady)

	if calls := provisioner.AndroidCalls(); len(calls) != 2 {
		t.Fatalf("AndroidCalls() = %#v, want two calls (initial failure + retry)", calls)
	}
}
