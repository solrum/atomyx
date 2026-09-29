package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/sidecar"
)

// goSidecarCaller wraps an in-process Go sidecar dispatcher as a
// Caller, so replaySidecarSteps can drive the Go sidecar directly,
// with no subprocess or wire encoding in between. It registers the
// seven feature groups cmd/atomyx-runtime wires that
// this harness exercises (meta, device, app, script, inspection,
// input, mirror-input) against a device.StaticProbe and a
// fixed-clock mock driver factory, and collects every event the
// dispatcher's handlers publish so the differential test can diff
// the event stream, not just responses.
//
// The harness never provisions a platform agent, so the ios/android
// agent status providers it hands to RegisterMirrorInputFeature
// always report idle: any mirrorInputService call in a replayed case
// fails with DriverNotReadyError, the same outcome a caller sees
// before it ever calls ensureIosAgent or ensureAndroidAgent.
type goSidecarCaller struct {
	dispatcher  *sidecar.Dispatcher
	session     *sidecar.Session
	unsubscribe func()

	mu     sync.Mutex
	events []Event
}

// newGoSidecarCaller composes a Go sidecar wired against a
// device.StaticProbe staged with snapshots and returns it alongside a
// close func that disposes the session. The caller stages no driver
// program: RunCase-style replay against this Caller must issue a
// selectDevice step before any step that depends on staged hierarchy,
// then the test stages the driver program directly onto the resulting
// mock.Driver (see stageMockDriver).
func newGoSidecarCaller(snapshots [][]device.Descriptor) (*goSidecarCaller, func()) {
	dispatcher := sidecar.NewDispatcher()
	events := sidecar.NewEventBus()
	session := sidecar.NewSession()
	ctx := &sidecar.Context{Dispatcher: dispatcher, Events: events, Session: session}

	probe := device.NewStaticProbe(snapshots...)
	factory := device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: orchestra.NewFakeClock(0)})

	// No AgentProvisioner exists in this harness: mirror-input's
	// readiness providers are backed by agent services that are never
	// told to Ensure, so every mirror action against them observes
	// idle, matching a caller that never provisioned an agent.
	iosAgentService := sidecar.NewIosAgentService(nil, events, nil)
	androidAgentService := sidecar.NewAndroidAgentService(nil, events, nil)

	handles := []sidecar.Handle{
		sidecar.RegisterMetaFeature(ctx),
		sidecar.RegisterDeviceFeature(ctx, probe, factory),
		sidecar.RegisterAppFeature(ctx),
		sidecar.RegisterScriptFeature(ctx),
		sidecar.RegisterInspectionFeature(ctx),
		sidecar.RegisterInputFeature(ctx),
		sidecar.RegisterMirrorInputFeature(ctx, sidecar.MirrorInputFeatureDeps{
			IosAgent:     iosAgentService,
			AndroidAgent: androidAgentService,
		}),
	}

	caller := &goSidecarCaller{dispatcher: dispatcher, session: session}
	caller.unsubscribe = events.Subscribe(func(e sidecar.Event) {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			payload = json.RawMessage("null")
		}
		caller.mu.Lock()
		caller.events = append(caller.events, Event{Name: e.Event, Payload: payload})
		caller.mu.Unlock()
	})

	closeFn := func() {
		caller.unsubscribe()
		for _, h := range handles {
			if h.Dispose != nil {
				_ = h.Dispose()
			}
		}
		session.Dispose()
	}
	return caller, closeFn
}

// Call implements Caller by dispatching directly against the Go
// sidecar's Dispatcher, converting a wire-level error Response into
// an *RPCError so replaySidecarSteps has one error shape to handle
// regardless of which Caller implementation it replays against.
func (c *goSidecarCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshaling params for %s: %w", method, err)
		}
		paramsRaw = raw
	}

	response := c.dispatcher.HandleRequest(ctx, sidecar.Request{ID: "1", Method: method, Params: paramsRaw})
	if response.Error != nil {
		var data json.RawMessage
		if response.Error.Data != nil {
			data, _ = json.Marshal(response.Error.Data)
		}
		return nil, &RPCError{Code: string(response.Error.Code), Message: response.Error.Message, Data: data}
	}
	result, err := json.Marshal(response.Result)
	if err != nil {
		return nil, fmt.Errorf("marshaling %s result: %w", method, err)
	}
	return result, nil
}

// Events returns every event published so far, in publish order.
func (c *goSidecarCaller) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

// stageMockDriver reaches into the currently selected device's driver
// and stages program onto it, mirroring go/internal/sidecar's own
// selectMockDevice test helper. Called after a selectDevice step, and
// a no-op when program is nil (a case with no device interaction,
// such as the meta group).
func stageMockDriver(session *sidecar.Session, program *RunnerDriverProgram) error {
	if program == nil {
		return nil
	}
	selection := session.Device()
	if selection == nil {
		return fmt.Errorf("stageMockDriver: no device selected")
	}
	drv, ok := selection.Driver.(*mock.Driver)
	if !ok {
		return fmt.Errorf("stageMockDriver: selected driver is %T, want *mock.Driver", selection.Driver)
	}
	if len(program.Apps) > 0 {
		apps := make([]driver.InstalledApp, len(program.Apps))
		for i, id := range program.Apps {
			apps[i] = driver.InstalledApp{BundleID: id}
		}
		drv.Apps = apps
	}
	for _, h := range program.Hierarchies {
		drv.StageHierarchy(h)
	}
	if program.Capabilities != nil {
		applyCapabilitiesOverride(drv, program.Capabilities)
	}
	if program.Screen != nil {
		drv.Screen = *program.Screen
	}
	return nil
}
