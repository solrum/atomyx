package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
)

// SelectKind is the device kind select_device reports back once a
// driver is built, and the legacy hint it still accepts on input. The
// input hint never influences resolution, because the device kind
// comes from the device's identity. The reported value is never
// empty, because Select always resolves one before returning.
// SelectKindEmulator can appear in the result even though input never
// accepts it, because Android distinguishes an emulator from a
// physical device.
type SelectKind string

const (
	SelectKindSimulator SelectKind = "simulator"
	SelectKindEmulator  SelectKind = "emulator"
	SelectKindDevice    SelectKind = "device"
)

// selectKindFromDriverKind maps the driver.DeviceKind a bound driver's
// own DeviceInfo call reports to the wire-level SelectKind returned
// to a caller. This is the primary source for the reported kind,
// because it reflects the device the driver actually connected to
// rather than the transport parameters Select chose before
// connecting.
func selectKindFromDriverKind(kind driver.DeviceKind) SelectKind {
	switch kind {
	case driver.DeviceKindSimulator:
		return SelectKindSimulator
	case driver.DeviceKindEmulator:
		return SelectKindEmulator
	default:
		return SelectKindDevice
	}
}

// selectKindFromDeviceKind maps a Descriptor's requested device.Kind
// to the wire-level SelectKind returned to a caller. Select uses this
// only as a fallback when the bound driver's DeviceInfo call fails,
// since the descriptor's kind reflects the connection parameters
// chosen before connecting, not necessarily the device that ends up
// bound.
func selectKindFromDeviceKind(kind device.Kind) SelectKind {
	switch kind {
	case device.KindSimulator:
		return SelectKindSimulator
	case device.KindEmulator:
		return SelectKindEmulator
	default:
		return SelectKindDevice
	}
}

// SelectDeviceInput is the DeviceSession.Select argument, decoded
// from the select_device tool's arguments.
type SelectDeviceInput struct {
	Platform device.Platform
	ID       string
	Kind     SelectKind
	Port     int
}

// ActiveDevice is the device currently bound to a DeviceSession.
type ActiveDevice struct {
	Platform    device.Platform
	ID          string
	Kind        SelectKind
	Orchestra   *orchestra.Orchestra
	Driver      driver.Driver
	ConnectedAt int64
}

// DeviceSessionDeps constructs a DeviceSession. Factory and Probe are
// required: Probe resolves a requested id to a device kind. Clock and Logger default to the system clock and a no-op logger.
type DeviceSessionDeps struct {
	Factory device.Factory
	Probe   device.Probe
	Clock   orchestra.Clock
	Logger  logging.Logger
}

// DeviceSession is the MCP server's active-device state: no device is
// bound at startup, select_device constructs one through Factory and
// swaps it in, and every device-touching tool reads Current() on each
// call rather than holding its own reference. This lets one server
// process drive any connected device on demand, including switching
// platforms mid-session without a restart.
type DeviceSession struct {
	mu      sync.Mutex
	active  *ActiveDevice
	closed  bool
	factory device.Factory
	probe   device.Probe
	clock   orchestra.Clock
	logger  logging.Logger
}

// errSessionClosed is returned by Select once the session has been
// closed.
var errSessionClosed = errors.New("session.select: session closed")

// NewDeviceSession constructs a DeviceSession from deps.
func NewDeviceSession(deps DeviceSessionDeps) *DeviceSession {
	clock := deps.Clock
	if clock == nil {
		clock = orchestra.SystemClock{}
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	return &DeviceSession{factory: deps.Factory, probe: deps.Probe, clock: clock, logger: logger}
}

// Current returns the currently-bound device, or nil when none is
// selected.
func (s *DeviceSession) Current() *ActiveDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Select tears down any previously-active driver, builds a new one
// through Factory for input, and marks it current. A disconnect
// failure on the previous driver is logged and does not block the new
// selection; a connect failure on the new driver leaves the session
// idle and is returned so select_device can report a structured
// failure. A Select that finishes while another Select's driver is
// already current releases that driver instead of orphaning it, and a
// Select on a closed session fails before building anything.
func (s *DeviceSession) Select(ctx context.Context, input SelectDeviceInput) (*ActiveDevice, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errSessionClosed
	}
	previous := s.active
	s.active = nil
	s.mu.Unlock()
	s.releasePrevious(previous)

	desc, err := s.resolveDescriptor(ctx, input)
	if err != nil {
		return nil, err
	}
	built, err := s.factory.Build(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf(
			"session.select: driver.connect failed for %s/%s: %w",
			input.Platform, input.ID, err,
		)
	}

	kind := selectKindFromDeviceKind(desc.Kind)
	if info, infoErr := built.Orchestra.DeviceInfo(ctx); infoErr == nil {
		kind = selectKindFromDriverKind(info.Kind)
	}

	active := &ActiveDevice{
		Platform:    input.Platform,
		ID:          input.ID,
		Kind:        kind,
		Orchestra:   built.Orchestra,
		Driver:      built.Driver,
		ConnectedAt: s.clock.NowMs(),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = built.Driver.Disconnect()
		return nil, errSessionClosed
	}
	displaced := s.active
	s.active = active
	s.mu.Unlock()
	s.releasePrevious(displaced)
	s.logger.Info("session.device_selected", logging.LogContext{
		"platform": string(input.Platform),
		"id":       input.ID,
		"kind":     string(active.Kind),
	})
	return active, nil
}

// releasePrevious disconnects a driver the session no longer
// tracks. A nil previous is a no-op, and a disconnect failure is
// logged because the replacement selection must proceed regardless.
func (s *DeviceSession) releasePrevious(previous *ActiveDevice) {
	if previous == nil {
		return
	}
	if err := previous.Driver.Disconnect(); err != nil {
		s.logger.Warn("session.previous_disconnect_failed", logging.LogContext{
			"id":       previous.ID,
			"platform": string(previous.Platform),
			"error":    err.Error(),
		})
		return
	}
	s.logger.Info("session.previous_disconnected", logging.LogContext{
		"id":       previous.ID,
		"platform": string(previous.Platform),
	})
}

// CheckHealth reports whether a device is currently bound and, when
// one is, whether its agent answers a bounded liveness probe. It
// never returns an error: no device bound is a normal report with
// Selected false, and an unresponsive agent is a normal report with
// Healthy false, since a failed probe is a check outcome, not a
// failure of the check itself.
func (s *DeviceSession) CheckHealth(ctx context.Context) orchestra.HealthReport {
	active := s.Current()
	if active == nil {
		return orchestra.HealthReport{Selected: false}
	}
	return active.Orchestra.CheckHealth(ctx)
}

// Close disconnects the active driver and makes every later Select
// release what it builds and fail, so a selection that was still
// launching when the server stopped cannot leave a child running.
// It is idempotent.
func (s *DeviceSession) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Disconnect()
}

// Disconnect tears down the active driver and marks the session idle.
// It is a no-op when the session is already idle.
func (s *DeviceSession) Disconnect() {
	s.mu.Lock()
	previous := s.active
	s.active = nil
	s.mu.Unlock()
	if previous == nil {
		return
	}
	if err := previous.Driver.Disconnect(); err != nil {
		s.logger.Warn("session.disconnect_failed", logging.LogContext{
			"id":       previous.ID,
			"platform": string(previous.Platform),
			"error":    err.Error(),
		})
		return
	}
	s.logger.Info("session.disconnected", logging.LogContext{
		"id":       previous.ID,
		"platform": string(previous.Platform),
	})
}

// resolveDescriptor builds the device.Descriptor Factory builds from
// by resolving the platform and id through device.ResolveDescriptor,
// the same path the CLI uses, so the device kind comes from the
// device's identity as the probe sees it. input.Kind is a legacy hint
// that is accepted on the wire and deliberately not consulted. An iOS
// id no scan finds is reported as DEVICE_NOT_FOUND with a remedy.
func (s *DeviceSession) resolveDescriptor(ctx context.Context, input SelectDeviceInput) (device.Descriptor, error) {
	desc, err := device.ResolveDescriptor(ctx, s.probe, input.Platform, input.ID)
	var notFound *device.DeviceNotFoundError
	if errors.As(err, &notFound) {
		return device.Descriptor{}, diag.New(
			diag.CodeDeviceNotFound,
			notFound.Error(),
			"Call list_devices to see the reachable iOS simulators and physical devices, then pass one of those ids. A physical iPhone must be connected over USB, unlocked, and trusted.",
			err,
		)
	}
	if err != nil {
		return device.Descriptor{}, err
	}
	desc.Port = input.Port
	return desc, nil
}
