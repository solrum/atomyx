package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"

	"dev.atomyx/internal/device"
)

// AndroidAgentState is the Atomyx Agent APK's provisioning lifecycle
// for one device serial, reported on the wire exactly as
// ensureAndroidAgent and androidAgentStatus return it and as the
// androidAgentStatus event carries it.
type AndroidAgentState string

const (
	AndroidAgentStateIdle       AndroidAgentState = "idle"
	AndroidAgentStateInstalling AndroidAgentState = "installing"
	AndroidAgentStateReady      AndroidAgentState = "ready"
	AndroidAgentStateFailed     AndroidAgentState = "failed"
)

// androidAgentPort is the Atomyx Agent APK's fixed control-channel
// port; every status reports it regardless of provisioning state.
const androidAgentPort = 8765

// AndroidAgentStatus is the ensureAndroidAgent and androidAgentStatus
// result shape, and the androidAgentStatus event payload.
type AndroidAgentStatus struct {
	Serial  string            `json:"serial"`
	State   AndroidAgentState `json:"state"`
	Message string            `json:"message,omitempty"`
	Port    int               `json:"port"`
}

type androidAgentEntry struct {
	state   AndroidAgentState
	message string
}

// AndroidAgentStatusProvider is the read-only seam mirror-input
// depends on to gate mirror actions on the Atomyx Agent APK's
// provisioning state, without depending on the concrete
// AndroidAgentService or its provisioning machinery. Satisfied by
// *AndroidAgentService.
type AndroidAgentStatusProvider interface {
	Status(serial string) AndroidAgentStatus
}

// AndroidAgentService owns the Atomyx Agent APK's provisioning state
// per device serial. Ensure is fire-and-forget: it returns the
// current status immediately and runs the provisioning call on a
// background goroutine, publishing an androidAgentStatus event
// whenever the state changes. A caller learns of a later transition
// by polling Status or subscribing to the event, never by blocking on
// Ensure.
type AndroidAgentService struct {
	provisioner device.AgentProvisioner
	events      *EventBus
	onPanic     PanicHandler

	mu      sync.Mutex
	entries map[string]*androidAgentEntry
}

// NewAndroidAgentService constructs an AndroidAgentService. The
// composition root builds one instance and shares it between
// RegisterAndroidAgentFeature (which drives provisioning) and
// RegisterMirrorInputFeature (which only reads readiness). onPanic
// receives a background provisioning goroutine's recovered panic, and
// passing nil discards it.
func NewAndroidAgentService(provisioner device.AgentProvisioner, events *EventBus, onPanic PanicHandler) *AndroidAgentService {
	return &AndroidAgentService{
		provisioner: provisioner,
		events:      events,
		onPanic:     onPanic,
		entries:     make(map[string]*androidAgentEntry),
	}
}

// Status reports serial's current provisioning state, defaulting to
// idle at the fixed port when serial has never been ensured.
func (s *AndroidAgentService) Status(serial string) AndroidAgentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(serial)
}

func (s *AndroidAgentService) statusLocked(serial string) AndroidAgentStatus {
	entry, ok := s.entries[serial]
	if !ok {
		return AndroidAgentStatus{Serial: serial, State: AndroidAgentStateIdle, Port: androidAgentPort}
	}
	return AndroidAgentStatus{Serial: serial, State: entry.state, Message: entry.message, Port: androidAgentPort}
}

// Ensure starts provisioning the Atomyx Agent APK for serial unless an
// entry already exists and is ready or installing, in which case the
// existing launch is left alone and its current status is returned
// unchanged. Otherwise a new installing entry replaces any prior one
// (including a failed one, so a retry after failure is a plain
// Ensure call) and the provisioning call runs on a background
// goroutine.
func (s *AndroidAgentService) Ensure(serial string) AndroidAgentStatus {
	s.mu.Lock()
	if entry, ok := s.entries[serial]; ok && (entry.state == AndroidAgentStateReady || entry.state == AndroidAgentStateInstalling) {
		status := s.statusLocked(serial)
		s.mu.Unlock()
		return status
	}
	s.entries[serial] = &androidAgentEntry{state: AndroidAgentStateInstalling}
	status := s.statusLocked(serial)
	s.mu.Unlock()
	s.emit(serial)

	go func() {
		defer s.recoverEnsurePanic(serial)
		if err := s.provisioner.EnsureAndroidAgent(context.Background(), serial); err != nil {
			s.markFailed(serial, err.Error())
			return
		}
		s.markReady(serial)
	}()

	return status
}

// recoverEnsurePanic stops a panic from provisioning serial's Atomyx
// Agent APK before it reaches the top of the goroutine, reports it
// through onPanic, and marks serial failed so a caller polling Status
// or subscribing to androidAgentStatus observes a failure instead of
// the goroutine simply vanishing. It must be the direct target of a
// defer statement: recover only stops a panic when called directly by
// the deferred function, not by a function the deferred function
// calls.
func (s *AndroidAgentService) recoverEnsurePanic(serial string) {
	r := recover()
	if r == nil {
		return
	}
	if s.onPanic != nil {
		s.onPanic("AndroidAgentService.Ensure", r, debug.Stack())
	}
	s.markFailed(serial, fmt.Sprintf("panic: %v", r))
}

func (s *AndroidAgentService) markReady(serial string) {
	s.mu.Lock()
	entry, ok := s.entries[serial]
	if !ok {
		s.mu.Unlock()
		return
	}
	entry.state = AndroidAgentStateReady
	entry.message = ""
	s.mu.Unlock()
	s.emit(serial)
}

func (s *AndroidAgentService) markFailed(serial, message string) {
	s.mu.Lock()
	entry, ok := s.entries[serial]
	if !ok {
		s.mu.Unlock()
		return
	}
	entry.state = AndroidAgentStateFailed
	entry.message = message
	s.mu.Unlock()
	s.emit(serial)
}

func (s *AndroidAgentService) emit(serial string) {
	s.events.Emit(Event{Event: "androidAgentStatus", Payload: s.Status(serial)})
}

// RegisterAndroidAgentFeature wires ensureAndroidAgent and
// androidAgentStatus against service. The composition root constructs
// service via NewAndroidAgentService so the same instance can also be
// injected into RegisterMirrorInputFeature as an
// AndroidAgentStatusProvider.
func RegisterAndroidAgentFeature(ctx *Context, service *AndroidAgentService) Handle {
	mustRegister(ctx.Dispatcher, "ensureAndroidAgent", func(_ context.Context, params json.RawMessage) (any, error) {
		serial, ok := decodeParams(params)["serial"].(string)
		if !ok || serial == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.serial must be a non-empty string", nil)
		}
		return service.Ensure(serial), nil //nolint:contextcheck // Ensure's provisioning goroutine deliberately outlives this RPC handler call, so it cannot use the handler's context without being canceled the moment the handler returns; see the doc comment on Ensure
	})

	mustRegister(ctx.Dispatcher, "androidAgentStatus", func(_ context.Context, params json.RawMessage) (any, error) {
		serial, ok := decodeParams(params)["serial"].(string)
		if !ok || serial == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.serial must be a non-empty string", nil)
		}
		return service.Status(serial), nil
	})

	return Handle{}
}
