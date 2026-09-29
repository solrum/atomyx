package sidecar

import (
	"context"
	"fmt"
	"sync"

	"dev.atomyx/internal/device"
)

// deviceService orchestrates device lifecycle: list, select,
// deselect. It does not construct drivers itself (device.Factory) or
// discover devices itself (device.Probe); it composes those two ports
// with the session that holds the currently selected device.
type deviceService struct {
	probe   device.Probe
	factory device.Factory
	session *Session
	events  *EventBus

	mu           sync.Mutex
	lastSnapshot []device.Descriptor

	// selectMu serializes selectKnown and Deselect end to end: the
	// check against the session's current device, the factory build,
	// and the session write all happen while it is held. Without it,
	// two concurrent selectKnown calls for the same id can both pass
	// the "already selected" check before either has built a driver,
	// so both call factory.Build, and the losing call's live driver
	// connection is never disposed. Since the agent's control channel
	// accepts only one live connection, that is a real resource leak,
	// not a cosmetic one.
	selectMu sync.Mutex
}

func newDeviceService(probe device.Probe, factory device.Factory, session *Session, events *EventBus) *deviceService {
	return &deviceService{probe: probe, factory: factory, session: session, events: events}
}

// List rescans for reachable devices, publishes deviceConnected,
// deviceStateChanged, and deviceDisconnected events for whatever
// changed since the previous scan, and remembers the result so a
// later Select can resolve an id without forcing a rescan.
func (s *deviceService) List(ctx context.Context) []device.Descriptor {
	snapshot := s.probe.Scan(ctx)
	s.mu.Lock()
	previous := s.lastSnapshot
	s.lastSnapshot = snapshot
	s.mu.Unlock()
	s.diffAndEmit(previous, snapshot)
	return snapshot
}

// diffAndEmit compares previous and next by device id and publishes
// one event per change: deviceConnected for an id newly present,
// deviceStateChanged for an id whose State differs, deviceDisconnected
// for an id no longer present. Events are emitted in the order
// devices appear in next (then previous, for disconnects), matching a
// host that renders device-list changes as they arrive.
func (s *deviceService) diffAndEmit(previous, next []device.Descriptor) {
	previousByID := make(map[string]device.Descriptor, len(previous))
	for _, d := range previous {
		previousByID[d.ID] = d
	}
	nextByID := make(map[string]device.Descriptor, len(next))
	for _, d := range next {
		nextByID[d.ID] = d
	}
	for _, d := range next {
		before, ok := previousByID[d.ID]
		switch {
		case !ok:
			s.events.Emit(Event{Event: "deviceConnected", Payload: d})
		case before.State != d.State:
			s.events.Emit(Event{Event: "deviceStateChanged", Payload: d})
		}
	}
	for _, d := range previous {
		if _, ok := nextByID[d.ID]; !ok {
			s.events.Emit(Event{Event: "deviceDisconnected", Payload: d})
		}
	}
}

// Select builds a live driver for the device identified by id and
// installs it as the session's current device. id is resolved
// against the most recent List snapshot; when it is not found there,
// Select rescans once before reporting the device unknown.
func (s *deviceService) Select(ctx context.Context, id string) (device.Descriptor, error) {
	found, ok := s.findInSnapshot(id)
	if !ok {
		refreshed := s.List(ctx)
		found, ok = findByID(refreshed, id)
		if !ok {
			return device.Descriptor{}, fmt.Errorf("Device %q not found", id)
		}
	}
	return s.selectKnown(ctx, found)
}

// Deselect clears the session's current device, disposing it.
func (s *deviceService) Deselect() {
	s.selectMu.Lock()
	defer s.selectMu.Unlock()
	s.session.SetDevice(nil)
}

func (s *deviceService) findInSnapshot(id string) (device.Descriptor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return findByID(s.lastSnapshot, id)
}

func findByID(descriptors []device.Descriptor, id string) (device.Descriptor, bool) {
	for _, d := range descriptors {
		if d.ID == id {
			return d, true
		}
	}
	return device.Descriptor{}, false
}

func (s *deviceService) selectKnown(ctx context.Context, desc device.Descriptor) (device.Descriptor, error) {
	// Holding selectMu across the check, the build, and the session
	// write is what makes selection atomic per id: a second concurrent
	// call for the same id blocks here until the first has installed
	// its selection, then takes the already-connected fast path below
	// instead of racing its own factory.Build.
	s.selectMu.Lock()
	defer s.selectMu.Unlock()

	current := s.session.Device()
	if current != nil && current.ID == desc.ID && current.Driver.IsConnected() {
		return desc, nil
	}
	if current != nil {
		// Dispose the outgoing device before building the new one so the
		// two never hold their respective platform control channels open
		// at the same time (an Android or iOS agent may only accept one
		// live connection).
		s.session.SetDevice(nil)
	}

	built, err := s.factory.Build(ctx, desc)
	if err != nil {
		return device.Descriptor{}, err
	}
	drv := built.Driver
	s.session.SetDevice(&DeviceSelection{
		ID:        desc.ID,
		Platform:  string(desc.Platform),
		Driver:    drv,
		Orchestra: built.Orchestra,
		Dispose:   drv.Disconnect,
	})
	return desc, nil
}
