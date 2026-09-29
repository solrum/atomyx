package sidecar

import (
	"errors"
	"sync"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/orchestra"
)

// ErrSessionNoDevice is returned by Session.RequireDevice when no
// device is currently selected. It is a plain Go error, distinct
// from the wire-level ErrNoDeviceSelected code: a handler that wants
// the latter on the wire wraps this error into a *DispatcherError
// itself.
var ErrSessionNoDevice = errors.New("sidecar: no device selected")

// DeviceSelection is the live device bound to the current session:
// its identity, its Driver and Orchestra, and the teardown hook that
// releases both. Platform is a plain string rather than a type
// borrowed from a feature package, because the session is
// infrastructure and must not depend on any specific feature's
// vocabulary.
type DeviceSelection struct {
	ID       string
	Platform string

	Driver    driver.Driver
	Orchestra *orchestra.Orchestra

	// Dispose releases the driver's resources. It must be safe to
	// call more than once. A nil Dispose is treated as a no-op.
	Dispose func() error
}

// Session is the sidecar's per-process mutable state: the currently
// selected device (with its Orchestra and cleanup hook) and the id
// of an in-flight run, so handlers can look them up without passing
// state through every parameter list. Session holds no business
// logic. Handlers read and mutate it directly.
//
// Session is safe for concurrent use.
type Session struct {
	mu          sync.Mutex
	device      *DeviceSelection
	activeRunID *string
}

// NewSession constructs a Session with no device selected and no
// active run.
func NewSession() *Session {
	return &Session{}
}

// Device returns the currently selected device, or nil when none is
// selected.
func (s *Session) Device() *DeviceSelection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.device
}

// RequireDevice returns the currently selected device, or
// ErrSessionNoDevice when none is selected.
func (s *Session) RequireDevice() (*DeviceSelection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.device == nil {
		return nil, ErrSessionNoDevice
	}
	return s.device, nil
}

// SetDevice installs selection as the current device. Selecting the
// device already active (matched by ID) is a no-op for the session's
// own state: the existing selection stays installed. Either way the
// selection that ends up discarded has its Dispose hook called, so
// that either the redundant equal-ID selection or the replaced
// previous selection releases its resources, never both installed
// selections leaking. Any error a Dispose hook returns is discarded,
// since a teardown failure must not block installing the new
// selection or releasing the redundant one. Passing a nil selection
// deselects the current device.
func (s *Session) SetDevice(selection *DeviceSelection) {
	s.mu.Lock()
	if s.device != nil && selection != nil && s.device.ID == selection.ID {
		s.mu.Unlock()
		disposeQuietly(selection)
		return
	}
	previous := s.device
	s.device = selection
	s.mu.Unlock()

	disposeQuietly(previous)
}

// ActiveRunID returns the id of the in-flight run, or nil when none
// is active.
func (s *Session) ActiveRunID() *string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeRunID
}

// SetActiveRunID records the id of the in-flight run. Passing nil
// clears it.
func (s *Session) SetActiveRunID(id *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeRunID = id
}

// Dispose clears the session's device and active run and releases
// the device's resources via its Dispose hook, with any error it
// returns discarded.
func (s *Session) Dispose() {
	s.mu.Lock()
	previous := s.device
	s.device = nil
	s.activeRunID = nil
	s.mu.Unlock()

	disposeQuietly(previous)
}

func disposeQuietly(selection *DeviceSelection) {
	if selection == nil || selection.Dispose == nil {
		return
	}
	_ = selection.Dispose()
}
