package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/device"
)

func subscribeIosAgentEvents(ctx *Context) (statuses func() []IosAgentStatus, unsubscribe func()) {
	var mu sync.Mutex
	var seen []IosAgentStatus
	unsub := ctx.Events.Subscribe(func(e Event) {
		if e.Event != "iosAgentStatus" {
			return
		}
		mu.Lock()
		seen = append(seen, e.Payload.(IosAgentStatus))
		mu.Unlock()
	})
	return func() []IosAgentStatus {
		mu.Lock()
		defer mu.Unlock()
		return append([]IosAgentStatus(nil), seen...)
	}, unsub
}

func waitForIosAgentState(t *testing.T, statuses func() []IosAgentStatus, want IosAgentState) IosAgentStatus {
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

// TestIosAgent_EnsureIosAgent_ProvisionerPanics_RecoversAndMarksFailed
// guards against a background provisioning panic crashing the whole
// process: the goroutine must recover, report through the injected
// PanicHandler, and mark the entry failed so a caller sees a failure
// instead of a silent hang. panickingProvisioner is defined in
// android_agent_test.go and shared across both agents' tests.
func TestIosAgent_EnsureIosAgent_ProvisionerPanics_RecoversAndMarksFailed(t *testing.T) {
	ctx := newTestContext()
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()

	var gotSource string
	var gotRecovered any
	var gotStack []byte
	onPanic := func(source string, recovered any, stack []byte) {
		gotSource = source
		gotRecovered = recovered
		gotStack = stack
	}
	RegisterIosAgentFeature(ctx, NewIosAgentService(panickingProvisioner{}, ctx.Events, onPanic), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "device"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	failed := waitForIosAgentState(t, statuses, IosAgentStateFailed)
	if failed.UDID != "udid-1" || failed.Message == "" {
		t.Fatalf("failed status = %#v, want udid-1 with a non-empty panic message", failed)
	}

	// onPanic and markFailed both run, in that order, on the same
	// background goroutine before markFailed's event publish. The
	// waitForIosAgentState call above already observed that publish,
	// so its side effects, including onPanic's, are visible here
	// without further synchronization.
	if gotSource != "IosAgentService.Ensure" {
		t.Fatalf("onPanic source = %q, want %q", gotSource, "IosAgentService.Ensure")
	}
	if gotRecovered != "ios agent panic" {
		t.Fatalf("onPanic recovered = %v, want %q", gotRecovered, "ios agent panic")
	}
	if len(gotStack) == 0 {
		t.Fatal("onPanic stack = empty, want a captured stack trace")
	}
}

func TestIosAgent_Status_UnknownUDID_ReturnsIdle(t *testing.T) {
	ctx := newTestContext()
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "iosAgentStatus", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(IosAgentStatus)
	want := IosAgentStatus{UDID: "udid-1", State: IosAgentStateIdle, Port: 22087}
	if !ok || got != want {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestIosAgent_EnsureIosAgent_MissingUDID_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"kind": "simulator"})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestIosAgent_EnsureIosAgent_InvalidKind_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "tablet"})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestIosAgent_EnsureIosAgent_ReturnsBuildingImmediately(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	gate := make(chan struct{})
	provisioner.SetGate(gate)
	defer close(gate)
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(IosAgentStatus)
	want := IosAgentStatus{UDID: "udid-1", State: IosAgentStateBuilding, Port: 22087}
	if !ok || got != want {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestIosAgent_EnsureIosAgent_Success_TransitionsToReadyAndEmits(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	ready := waitForIosAgentState(t, statuses, IosAgentStateReady)
	want := IosAgentStatus{UDID: "udid-1", State: IosAgentStateReady, Port: 22087}
	if ready != want {
		t.Fatalf("ready status = %#v, want %#v", ready, want)
	}

	statusResult, err := ctx.Dispatcher.Dispatch(context.Background(), "iosAgentStatus", params)
	if err != nil {
		t.Fatalf("iosAgentStatus: Dispatch() error = %v", err)
	}
	if statusResult.(IosAgentStatus) != want {
		t.Fatalf("iosAgentStatus result = %#v, want %#v", statusResult, want)
	}

	calls := provisioner.IOSCalls()
	if len(calls) != 1 || calls[0] != (device.FakeIOSCall{UDID: "udid-1", Kind: device.IosAgentKindSimulator}) {
		t.Fatalf("IOSCalls() = %#v, want one simulator call for udid-1", calls)
	}
}

func TestIosAgent_EnsureIosAgent_Failure_TransitionsToFailedWithMessage(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	provisioner.SetIOSResult("udid-1", errors.New("boom"))
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "device"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	failed := waitForIosAgentState(t, statuses, IosAgentStateFailed)
	want := IosAgentStatus{UDID: "udid-1", State: IosAgentStateFailed, Message: "boom", Port: 22087}
	if failed != want {
		t.Fatalf("failed status = %#v, want %#v", failed, want)
	}
}

func TestIosAgent_EnsureIosAgent_WhileBuilding_DedupsWithoutRestarting(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	gate := make(chan struct{})
	provisioner.SetGate(gate)
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	first, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	if err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	waitForIOSCallCount(t, provisioner, 1)

	second, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	if err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	if first.(IosAgentStatus) != second.(IosAgentStatus) {
		t.Fatalf("first = %#v, second = %#v, want equal building statuses", first, second)
	}
	close(gate)

	if calls := provisioner.IOSCalls(); len(calls) != 1 {
		t.Fatalf("IOSCalls() = %#v, want exactly one call while building", calls)
	}
}

func waitForIOSCallCount(t *testing.T, provisioner *device.FakeProvisioner, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if len(provisioner.IOSCalls()) >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("IOSCalls() did not reach %d within timeout", want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestIosAgent_EnsureIosAgent_WhileReady_DedupsWithoutRestarting(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForIosAgentState(t, statuses, IosAgentStateReady)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params)
	if err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	want := IosAgentStatus{UDID: "udid-1", State: IosAgentStateReady, Port: 22087}
	if result.(IosAgentStatus) != want {
		t.Fatalf("second ensure result = %#v, want %#v", result, want)
	}
	if calls := provisioner.IOSCalls(); len(calls) != 1 {
		t.Fatalf("IOSCalls() = %#v, want exactly one call once ready", calls)
	}
}

// TestIosAgent_Dispose_KillsEveryReadyDriverProcess guards the
// orphaned-process regression at the service level: once an agent
// reaches ready, its underlying driver process must actually be
// stopped when the service is disposed, not merely have a kill
// function sitting unused.
func TestIosAgent_Dispose_KillsEveryReadyDriverProcess(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	service := NewIosAgentService(provisioner, ctx.Events, nil)
	RegisterIosAgentFeature(ctx, service, device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForIosAgentState(t, statuses, IosAgentStateReady)

	if got := provisioner.IOSKillCount("udid-1"); got != 0 {
		t.Fatalf("IOSKillCount() = %d before Dispose, want 0", got)
	}
	if err := service.Dispose(); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if got := provisioner.IOSKillCount("udid-1"); got != 1 {
		t.Fatalf("IOSKillCount() = %d after Dispose, want exactly 1", got)
	}
}

// TestIosAgent_Dispose_NeverStartedOrFailed_DoesNotPanic guards Dispose
// against an entry that never reached ready (idle, never ensured, or
// failed): none of those carries a kill function, and Dispose must
// skip them instead of invoking a nil one.
func TestIosAgent_Dispose_NeverStartedOrFailed_DoesNotPanic(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	provisioner.SetIOSResult("udid-1", errors.New("boom"))
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	service := NewIosAgentService(provisioner, ctx.Events, nil)
	RegisterIosAgentFeature(ctx, service, device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForIosAgentState(t, statuses, IosAgentStateFailed)

	if err := service.Dispose(); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
}

func TestIosAgent_EnsureIosAgent_AfterFailure_Retries(t *testing.T) {
	ctx := newTestContext()
	provisioner := device.NewFakeProvisioner()
	provisioner.SetIOSResult("udid-1", errors.New("boom"))
	statuses, unsubscribe := subscribeIosAgentEvents(ctx)
	defer unsubscribe()
	RegisterIosAgentFeature(ctx, NewIosAgentService(provisioner, ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1", "kind": "simulator"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	waitForIosAgentState(t, statuses, IosAgentStateFailed)

	provisioner.SetIOSResult("udid-1", nil)
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "ensureIosAgent", params); err != nil {
		t.Fatalf("retry Dispatch() error = %v", err)
	}
	waitForIosAgentState(t, statuses, IosAgentStateReady)

	if calls := provisioner.IOSCalls(); len(calls) != 2 {
		t.Fatalf("IOSCalls() = %#v, want two calls (initial failure + retry)", calls)
	}
}

func TestIosAgent_SimHidStatus_UnknownUDID_ReturnsIdle(t *testing.T) {
	ctx := newTestContext()
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{"udid": "udid-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "iosSimHidStatus", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(SimHidStatus)
	want := SimHidStatus{UDID: "udid-1", State: device.SimHidStateIdle}
	if !ok || !simHidStatusEqual(got, want) {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestIosAgent_SimHidStatus_MissingUDID_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), device.NewFakeSimHidStatusProvider())

	params, _ := json.Marshal(map[string]string{})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "iosSimHidStatus", params)
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestIosAgent_SimHidStatus_ReflectsProviderState(t *testing.T) {
	ctx := newTestContext()
	simHid := device.NewFakeSimHidStatusProvider()
	simHid.SetStatus("udid-1", device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateReady, Port: 19876, HasPort: true})
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), simHid)

	params, _ := json.Marshal(map[string]string{"udid": "udid-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "iosSimHidStatus", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(SimHidStatus)
	want := SimHidStatus{UDID: "udid-1", State: device.SimHidStateReady, Port: intPtr(19876)}
	if !ok || !simHidStatusEqual(got, want) {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func TestIosAgent_SimHidStatus_ReflectsFailedState(t *testing.T) {
	ctx := newTestContext()
	simHid := device.NewFakeSimHidStatusProvider()
	simHid.SetStatus("udid-1", device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateFailed, Message: "boom"})
	RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), simHid)

	params, _ := json.Marshal(map[string]string{"udid": "udid-1"})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "iosSimHidStatus", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(SimHidStatus)
	want := SimHidStatus{UDID: "udid-1", State: device.SimHidStateFailed, Message: "boom"}
	if !ok || !simHidStatusEqual(got, want) {
		t.Fatalf("Dispatch() result = %#v, want %#v", result, want)
	}
}

func intPtr(v int) *int {
	return &v
}

// simHidStatusEqual compares two SimHidStatus values by the port they
// designate rather than by pointer identity, since two independently
// constructed non-nil *int fields holding the same value are never
// == to each other.
func simHidStatusEqual(a, b SimHidStatus) bool {
	if a.UDID != b.UDID || a.State != b.State || a.Message != b.Message {
		return false
	}
	if (a.Port == nil) != (b.Port == nil) {
		return false
	}
	return a.Port == nil || *a.Port == *b.Port
}

// TestIosAgent_SimHidStatus_MarshalsPortPresenceFaithfully marshals
// the dispatcher result to JSON, because a struct-only comparison
// cannot catch `omitempty` silently dropping a present port of 0: a
// ready status with a real port must marshal the port, a ready status
// whose port happens to be 0 must still marshal it, and every other
// state must omit the field rather than emit port 0.
func TestIosAgent_SimHidStatus_MarshalsPortPresenceFaithfully(t *testing.T) {
	cases := []struct {
		name       string
		status     device.SimHidStatus
		wantInJSON string
		portField  bool
	}{
		{
			name:       "ready with a real port",
			status:     device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateReady, Port: 19876, HasPort: true},
			wantInJSON: `"port":19876`,
			portField:  true,
		},
		{
			name:       "ready with port zero",
			status:     device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateReady, Port: 0, HasPort: true},
			wantInJSON: `"port":0`,
			portField:  true,
		},
		{
			name:      "idle",
			status:    device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateIdle},
			portField: false,
		},
		{
			name:      "spawning",
			status:    device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateSpawning},
			portField: false,
		},
		{
			name:      "failed",
			status:    device.SimHidStatus{UDID: "udid-1", State: device.SimHidStateFailed, Message: "boom"},
			portField: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext()
			simHid := device.NewFakeSimHidStatusProvider()
			simHid.SetStatus("udid-1", tc.status)
			RegisterIosAgentFeature(ctx, NewIosAgentService(device.NewFakeProvisioner(), ctx.Events, nil), simHid)

			params, _ := json.Marshal(map[string]string{"udid": "udid-1"})
			result, err := ctx.Dispatcher.Dispatch(context.Background(), "iosSimHidStatus", params)
			if err != nil {
				t.Fatalf("Dispatch() error = %v", err)
			}

			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			_, hasPortField := decoded["port"]
			if hasPortField != tc.portField {
				t.Fatalf("json = %s, port field present = %v, want %v", encoded, hasPortField, tc.portField)
			}
			if tc.portField && !strings.Contains(string(encoded), tc.wantInJSON) {
				t.Fatalf("json = %s, want it to contain %s", encoded, tc.wantInJSON)
			}
		})
	}
}
