package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync"

	"dev.atomyx/internal/device"
)

// IosAgentState is the XCUITest driver's provisioning lifecycle for
// one device UDID, reported on the wire exactly as ensureIosAgent and
// iosAgentStatus return it and as the iosAgentStatus event carries it.
type IosAgentState string

const (
	IosAgentStateIdle     IosAgentState = "idle"
	IosAgentStateBuilding IosAgentState = "building"
	IosAgentStateReady    IosAgentState = "ready"
	IosAgentStateFailed   IosAgentState = "failed"
)

// iosAgentPort is the XCUITest driver's fixed control-channel port;
// every status reports it regardless of provisioning state.
const iosAgentPort = 22087

// IosAgentStatus is the ensureIosAgent and iosAgentStatus result
// shape, and the iosAgentStatus event payload.
type IosAgentStatus struct {
	UDID    string        `json:"udid"`
	State   IosAgentState `json:"state"`
	Message string        `json:"message,omitempty"`
	Port    int           `json:"port"`
}

// SimHidStatus is the iosSimHidStatus result shape, matching the
// TypeScript sidecar's SimHidStatus field names and device.SimHidState
// values byte for byte. Port is a pointer so a helper that has reached
// the ready state serializes its port even when the port happens to
// be 0, while every other state omits the field entirely. This is the same
// distinction TypeScript's optional `port?: number` makes by leaving
// the field unset rather than assigning it 0.
type SimHidStatus struct {
	UDID    string             `json:"udid"`
	State   device.SimHidState `json:"state"`
	Port    *int               `json:"port,omitempty"`
	Message string             `json:"message,omitempty"`
}

// SimHidStatusProvider is the read-only seam iosSimHidStatus depends
// on: it never spawns anything itself, it only reports whatever the
// device package's SimHidManager currently knows about udid. Keeping
// this a pure query on an injected interface, instead of sidecar
// calling into device.SimHidManager's StartSimHid directly, keeps a
// status check from ever having the side effect of starting a
// process. Spawning happens only during device selection, in the
// driver factory.
type SimHidStatusProvider interface {
	Status(udid string) device.SimHidStatus
}

type iosAgentEntry struct {
	state   IosAgentState
	message string
	// kill stops the XCUITest driver process markReady's caller
	// brought up, set only once that provisioning call has succeeded.
	// Dispose invokes every entry's kill so the process does not
	// outlive this service.
	kill func() error
}

// IosAgentStatusProvider is the read-only seam mirror-input depends
// on to gate mirror actions on the XCUITest agent's provisioning
// state, without depending on the concrete IosAgentService or its
// provisioning machinery. Satisfied by *IosAgentService.
type IosAgentStatusProvider interface {
	Status(udid string) IosAgentStatus
}

// IosAgentService owns the XCUITest agent's provisioning state per
// device UDID. Ensure is fire-and-forget: it returns the current
// status immediately and runs the provisioning call on a background
// goroutine, publishing an iosAgentStatus event whenever the state
// changes. A caller learns of a later transition by polling Status or
// subscribing to the event, never by blocking on Ensure.
type IosAgentService struct {
	provisioner device.AgentProvisioner
	events      *EventBus
	onPanic     PanicHandler

	// ctx bounds every provisioning call, and Dispose cancels it so an
	// agent still building or launching is stopped instead of orphaned.
	ctx    context.Context
	cancel context.CancelFunc
	// inflight counts provisioning goroutines so Dispose can wait until
	// each has stopped the process it was bringing up.
	inflight sync.WaitGroup

	mu       sync.Mutex
	disposed bool
	entries  map[string]*iosAgentEntry
}

// NewIosAgentService constructs an IosAgentService. The composition
// root builds one instance and shares it between RegisterIosAgentFeature
// (which drives provisioning) and RegisterMirrorInputFeature (which
// only reads readiness), mirroring how SimHidStatusProvider is shared
// between the driver factory and RegisterIosAgentFeature. onPanic
// receives a background provisioning goroutine's recovered panic, and
// passing nil discards it.
func NewIosAgentService(provisioner device.AgentProvisioner, events *EventBus, onPanic PanicHandler) *IosAgentService {
	ctx, cancel := context.WithCancel(context.Background())
	return &IosAgentService{
		provisioner: provisioner,
		events:      events,
		onPanic:     onPanic,
		ctx:         ctx,
		cancel:      cancel,
		entries:     make(map[string]*iosAgentEntry),
	}
}

// Status reports udid's current provisioning state, defaulting to
// idle at the fixed port when udid has never been ensured.
func (s *IosAgentService) Status(udid string) IosAgentStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(udid)
}

func (s *IosAgentService) statusLocked(udid string) IosAgentStatus {
	entry, ok := s.entries[udid]
	if !ok {
		return IosAgentStatus{UDID: udid, State: IosAgentStateIdle, Port: iosAgentPort}
	}
	return IosAgentStatus{UDID: udid, State: entry.state, Message: entry.message, Port: iosAgentPort}
}

// Ensure starts provisioning the XCUITest agent for udid unless an
// entry already exists and is ready or building, in which case the
// existing launch is left alone and its current status is returned
// unchanged. Otherwise a new building entry replaces any prior one
// (including a failed one, so a retry after failure is a plain
// Ensure call) and the provisioning call runs on a background
// goroutine.
func (s *IosAgentService) Ensure(udid string, kind device.IosAgentKind) IosAgentStatus {
	s.mu.Lock()
	if entry, ok := s.entries[udid]; s.disposed || (ok && (entry.state == IosAgentStateReady || entry.state == IosAgentStateBuilding)) {
		status := s.statusLocked(udid)
		s.mu.Unlock()
		return status
	}
	s.entries[udid] = &iosAgentEntry{state: IosAgentStateBuilding}
	status := s.statusLocked(udid)
	s.inflight.Add(1)
	s.mu.Unlock()
	s.emit(udid)

	go func() {
		defer s.inflight.Done()
		defer s.recoverEnsurePanic(udid)
		kill, err := s.provisioner.EnsureIOSAgent(s.ctx, udid, kind)
		if err != nil {
			s.markFailed(udid, err.Error())
			return
		}
		s.markReady(udid, kill)
	}()

	return status
}

// recoverEnsurePanic stops a panic from provisioning udid's XCUITest
// agent before it reaches the top of the goroutine, reports it
// through onPanic, and marks udid failed so a caller polling Status
// or subscribing to iosAgentStatus observes a failure instead of the
// goroutine simply vanishing. It must be the direct target of a defer
// statement: recover only stops a panic when called directly by the
// deferred function, not by a function the deferred function calls.
func (s *IosAgentService) recoverEnsurePanic(udid string) {
	r := recover()
	if r == nil {
		return
	}
	if s.onPanic != nil {
		s.onPanic("IosAgentService.Ensure", r, debug.Stack())
	}
	s.markFailed(udid, fmt.Sprintf("panic: %v", r))
}

func (s *IosAgentService) markReady(udid string, kill func() error) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		_ = kill()
		return
	}
	entry, ok := s.entries[udid]
	if !ok {
		s.mu.Unlock()
		return
	}
	entry.state = IosAgentStateReady
	entry.message = ""
	entry.kill = kill
	s.mu.Unlock()
	s.emit(udid)
}

// Dispose stops every XCUITest driver process this service brought
// up, so none of them outlives the service. A provisioning call still
// building or launching is canceled and awaited, and a later Ensure
// is a no-op. Each entry's kill is invoked at most once and its error
// discarded, because one device's teardown failure must not block
// releasing the rest. Safe to call during process shutdown;
// RegisterIosAgentFeature wires it as the feature's Handle.Dispose.
func (s *IosAgentService) Dispose() error {
	s.mu.Lock()
	s.disposed = true
	s.mu.Unlock()
	s.cancel()
	s.inflight.Wait()

	s.mu.Lock()
	kills := make([]func() error, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.kill != nil {
			kills = append(kills, entry.kill)
		}
	}
	s.mu.Unlock()

	for _, kill := range kills {
		_ = kill()
	}
	return nil
}

func (s *IosAgentService) markFailed(udid, message string) {
	s.mu.Lock()
	entry, ok := s.entries[udid]
	if !ok {
		s.mu.Unlock()
		return
	}
	entry.state = IosAgentStateFailed
	entry.message = message
	s.mu.Unlock()
	s.emit(udid)
}

func (s *IosAgentService) emit(udid string) {
	s.events.Emit(Event{Event: "iosAgentStatus", Payload: s.Status(udid)})
}

// RegisterIosAgentFeature wires ensureIosAgent and iosAgentStatus
// against service, and iosSimHidStatus against simHid. The returned
// Handle's Dispose is service.Dispose, so the composition root's
// shutdown path stops every XCUITest driver process this service
// brought up. The composition root constructs service via
// NewIosAgentService so the same instance can also be injected into
// RegisterMirrorInputFeature as an IosAgentStatusProvider.
func RegisterIosAgentFeature(ctx *Context, service *IosAgentService, simHid SimHidStatusProvider) Handle {
	mustRegister(ctx.Dispatcher, "ensureIosAgent", func(_ context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		udid, ok := p["udid"].(string)
		if !ok || udid == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.udid must be a non-empty string", nil)
		}
		kindRaw, _ := p["kind"].(string)
		kind := device.IosAgentKind(kindRaw)
		if kind != device.IosAgentKindSimulator && kind != device.IosAgentKindDevice {
			return nil, NewDispatcherError(ErrInvalidParams, `params.kind must be "simulator" or "device"`, nil)
		}
		return service.Ensure(udid, kind), nil //nolint:contextcheck // Ensure's provisioning goroutine deliberately outlives this RPC handler call, so it cannot use the handler's context without being canceled the moment the handler returns; see the doc comment on Ensure
	})

	mustRegister(ctx.Dispatcher, "iosAgentStatus", func(_ context.Context, params json.RawMessage) (any, error) {
		udid, ok := decodeParams(params)["udid"].(string)
		if !ok || udid == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.udid must be a non-empty string", nil)
		}
		return service.Status(udid), nil
	})

	mustRegister(ctx.Dispatcher, "iosSimHidStatus", func(_ context.Context, params json.RawMessage) (any, error) {
		udid, ok := decodeParams(params)["udid"].(string)
		if !ok || udid == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.udid must be a non-empty string", nil)
		}
		status := simHid.Status(udid)
		return SimHidStatus{UDID: status.UDID, State: status.State, Port: simHidWirePort(status), Message: status.Message}, nil
	})

	return Handle{Dispose: service.Dispose}
}

// simHidWirePort converts status's present-or-absent port into the
// wire pointer shape: non-nil, carrying the port value even when it
// is 0, whenever status.HasPort is true, and nil when status has no
// port to report.
func simHidWirePort(status device.SimHidStatus) *int {
	if !status.HasPort {
		return nil
	}
	port := status.Port
	return &port
}
