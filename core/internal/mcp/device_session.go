package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
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

// DeviceSessionDeps constructs a DeviceSession. Probe resolves a
// requested id to a device kind, and Runners names the device of an
// already-served iOS runner when no id is given. Session carries the dependencies of
// session.Open, and its Factory defaults to Factory, its Clock to Clock
// and its Logger to Logger when unset. Clock and Logger default to the
// system clock and a no-op logger. TimerClock drives the acquisition
// budget and the idle timers and defaults to the system clock. It is
// separate from Clock because a test clock that lets every sleep return
// at once would otherwise expire every timer immediately. Timeouts fields left at zero take
// their defaults. Runs lets the session see and close the server's open
// run, and may be nil for a host with no run tracking.
type DeviceSessionDeps struct {
	Factory    device.WrappingFactory
	Probe      device.Probe
	Runners    device.RunnerIdentifier
	Clock      orchestra.Clock
	TimerClock clock.Clock
	Logger     logging.Logger
	Session    session.Deps
	Timeouts   session.Timeouts
	Runs       *runSession
	// Pinned binds the session to one device, nil when unbound.
	Pinned *SelectDeviceInput
}

// holderState is where the session stands in the holder state machine
// that governs when it owns its device.
type holderState string

const (
	// holderUnselected: no device was ever selected, or the agent
	// disconnected it explicitly. Only select_device leaves this state.
	holderUnselected holderState = "unselected"
	// holderIdle: a device is held, no call is in flight and no run is
	// open. The idle timer runs.
	holderIdle holderState = "idle"
	// holderRunning: a device is held and a call is in flight or a run is
	// open.
	holderRunning holderState = "running"
	// holderReleased: the session no longer holds the device, either
	// because the idle timer gave it up or because the process it was
	// handed to has finished. releasedBy tells which. The descriptor is
	// remembered, so the next device-needing call re-acquires it.
	holderReleased holderState = "released"
	// holderBorrowed: the session handed its device to another process
	// that asked for it, and watches that process until it ends. The
	// descriptor is remembered like in holderReleased.
	holderBorrowed holderState = "borrowed"
	// holderLost: the session had the device and lost it without
	// choosing to. The descriptor is remembered like in holderReleased.
	holderLost holderState = "lost"
)

// releaseCause names why a session is in holderReleased.
type releaseCause int

const (
	releasedIdle releaseCause = iota
	releasedHandedOver
)

// timerKind names which idle timeout is armed.
type timerKind int

const (
	timerIdle timerKind = iota
	timerOpenRun
)

// rememberedDevice is what a session keeps after giving its device up,
// so a later call can take the same device again.
type rememberedDevice struct {
	desc   device.Descriptor
	launch session.LaunchState
}

// expiredRun marks a run the open-run idle timeout closed.
type expiredRun struct {
	id string
	// notified reports that a device-needing call already received
	// RUN_EXPIRED for this run.
	notified bool
}

// DeviceSession is the MCP server's active-device state: no device is
// bound at startup, select_device acquires one through session.Open, and
// every device-touching tool resolves it through Ensure on each call
// rather than holding its own reference. This lets one server process
// drive any connected device on demand, including switching platforms
// mid-session without a restart.
//
// The session gives its device up after an idle timeout and takes it
// again on the next call that needs it. The states and transitions are
// those of holderState: a held device is idle or running, a given-up one
// is released, borrowed or lost, and the descriptor is kept for those three.
type DeviceSession struct {
	// acquireMu serializes every transition of sess: Select, Ensure,
	// Disconnect, onTimer and watchLost each hold it from the moment they
	// decide on a transition until it is installed or torn down. The slow
	// runner build runs before the lock is taken, and each transition
	// re-reads the state under the lock, so a change made while it built
	// is seen. mu alone guards reads of the fields below.
	acquireMu transitionLock

	mu     sync.Mutex
	closed bool
	state  holderState
	// releasedBy is meaningful while state is holderReleased.
	releasedBy releaseCause
	// sess is the held session. Writing it requires acquireMu and mu.
	sess       *session.Session
	active     *ActiveDevice
	remembered *rememberedDevice
	inFlight   int
	expired    *expiredRun
	timerGen   uint64
	timerStop  context.CancelFunc
	// borrowGen numbers the yields this session accepted. borrowStop ends the
	// watch of the newest borrower and is nil when none is watched. yields
	// counts the accepted yields whose follow-up has not finished.
	borrowGen  uint64
	borrowStop context.CancelFunc
	// borrowerHolder describes the process that took the device, in the
	// words DEVICE_HELD uses. It is empty until that process is watched.
	borrowerHolder string
	yields         sync.WaitGroup

	probe   device.Probe
	runners device.RunnerIdentifier
	clock   orchestra.Clock
	timers  clock.Clock
	// timerBase is the parent of every idle timer's context. It is never
	// canceled, so only a replaced or stopped timer ends early.
	timerBase context.Context
	// life ends when Close runs, so an acquisition in flight stops
	// instead of delaying shutdown.
	life       context.Context
	cancelLife context.CancelCauseFunc
	logger     logging.Logger
	deps       session.Deps
	timeouts   session.Timeouts
	runs       *runSession
	// pinned is the only device Select accepts, nil when unbound.
	pinned *SelectDeviceInput
	// pinMu serializes the first-use selection of the pinned device, so
	// concurrent first calls select it once.
	pinMu sync.Mutex
	// gate keeps a fresh capture of any session this host holds from
	// interleaving with a tool call. It outlives each held session.
	gate *session.ActivityGate

	// recMu guards the three fields below. It is a leaf lock, so a run
	// session may call SetInterventionRecorder while it holds its own lock.
	recMu sync.Mutex
	// recorder is where the open run keeps interventions, nil when no run
	// is routed.
	recorder session.InterventionRecorder
	// recSess is the newest installed session, which receives recorder.
	recSess *session.Session

	// pendMu guards carried. It is a leaf lock.
	pendMu sync.Mutex
	// carried holds the entries of sessions this host no longer holds,
	// kept until the next result drains them.
	carried []session.Intervention
}

// maxCarriedInterventions bounds the entries kept for sessions this host
// gave up, so a host whose agent never calls keeps only the newest.
const maxCarriedInterventions = 256

// errSessionClosed is returned by Select once the session has been
// closed.
// selectBorrower is the command string a holder shows to its own user when
// select_device takes its device.
const selectBorrower = "atomyx-mcp select_device"

var errSessionClosed = errors.New("session.select: session closed")

// toolsOnTheRun are the tools that only make sense while the run they
// belong to is open. After the open-run idle timeout closes a run, they
// report RUN_EXPIRED until a new run starts.
var toolsOnTheRun = map[string]bool{"finish_run": true, "report_bug": true}

// toolsNeedingTheDevice are the tools that drive the device. The first
// of them to run after a run expired reports RUN_EXPIRED once, so the
// agent learns its run is gone instead of silently driving a new
// device acquisition outside any run.
var toolsNeedingTheDevice = map[string]bool{
	"list_apps": true, "launch_app": true, "get_ui_tree": true, "find_element": true,
	"screenshot": true, "tap": true, "tap_and_wait_transition": true, "input_text": true,
	"swipe": true, "press_key": true, "wait_for_element": true, "run_script": true,
}

// NewDeviceSession constructs a DeviceSession from deps.
func NewDeviceSession(deps DeviceSessionDeps) *DeviceSession {
	nowClock := deps.Clock
	if nowClock == nil {
		nowClock = orchestra.SystemClock{}
	}
	timers := deps.TimerClock
	if timers == nil {
		timers = clock.SystemClock{}
	}
	logger := deps.Logger
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	sessionDeps := deps.Session
	if sessionDeps.Factory == nil {
		sessionDeps.Factory = deps.Factory
	}
	if sessionDeps.Clock == nil {
		sessionDeps.Clock = timers
	}
	if sessionDeps.Logger == nil {
		sessionDeps.Logger = logger
	}
	if sessionDeps.Command == "" {
		sessionDeps.Command = "atomyx-mcp"
	}
	if sessionDeps.Role == "" {
		sessionDeps.Role = "mcp"
	}
	life, cancelLife := context.WithCancelCause(context.Background())
	return &DeviceSession{
		life: life, cancelLife: cancelLife,
		state: holderUnselected, probe: deps.Probe, runners: deps.Runners, clock: nowClock, timers: timers, timerBase: context.Background(), logger: logger,
		deps: sessionDeps, timeouts: deps.Timeouts.WithDefaults(), runs: deps.Runs, pinned: deps.Pinned,
		gate: session.NewActivityGate(),
	}
}

// Current returns the currently-bound device, or nil when none is
// held. A session that released or lost its device has none bound, and
// Ensure takes it again.
func (s *DeviceSession) Current() *ActiveDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// SelectedDescriptor returns the descriptor of the selected device, or
// nil when none was ever selected. A released or lost device keeps its
// descriptor.
func (s *DeviceSession) SelectedDescriptor() *device.Descriptor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remembered == nil {
		return nil
	}
	desc := s.remembered.desc
	return &desc
}

// Select acquires the device named by input through session.Open and
// marks it current. The new device is resolved, prepared and opened
// before the current one is torn down, so a failed selection keeps the
// working device. The one exception is a request that reaches the
// hardware already held, even through another id such as a second serial
// of one phone, whose lease must be given up before it can be taken
// again. The acquisition runs outside the tool call's time budget, and
// the runner build runs outside acquireMu, so an idle release or a lost
// device is not delayed by it. A disconnect failure on the previous
// driver is logged and does not block the new selection. A failed
// re-take of the held hardware leaves it remembered, so the next call
// that needs it takes it again. A failed acquisition is returned, so
// select_device can report a structured failure, for example DEVICE_HELD
// when another process drives the device. A canceled ctx returns its own
// error. A Select that finishes on a closed session releases what it
// acquired.
//
// Select is the agent's explicit choice of a device, so when another
// process holds the device and can give it up because it is idle, the
// device is taken from it and that process is told who took it. A device
// held by a busy process fails with DEVICE_HELD naming the holder.
func (s *DeviceSession) Select(ctx context.Context, input SelectDeviceInput) (*ActiveDevice, error) {
	return s.selectDevice(ctx, input, true)
}

// selectDevice is Select with the choice of whether a held device may be
// borrowed. The automatic selection of a pinned device never borrows,
// because no agent asked for it.
func (s *DeviceSession) selectDevice(ctx context.Context, input SelectDeviceInput, borrow bool) (*ActiveDevice, error) {
	if s.isClosed() {
		return nil, errSessionClosed
	}
	if err := s.checkPinned(input); err != nil {
		return nil, err
	}
	defer pauseToolBudget(ctx)()

	desc, err := s.resolveDescriptor(ctx, input)
	if err != nil {
		return nil, err
	}
	identity, err := s.deps.ResolveIdentity(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("session.select: driver.connect failed for %s/%s: %w", input.Platform, input.ID, err)
	}
	ctx, stop := s.withLife(ctx)
	defer stop()
	if err := s.prepare(ctx, desc); err != nil {
		return nil, fmt.Errorf("session.select: driver.connect failed for %s/%s: %w", input.Platform, input.ID, err)
	}

	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()
	if s.isClosed() {
		return nil, errSessionClosed
	}
	s.mu.Lock()
	held := s.sess
	s.mu.Unlock()
	if held != nil && held.Identity() == identity {
		previous := s.detach(false)
		s.releasePrevious(previous) //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
		s.rememberLaunch(previous)
	}
	sess, err := s.open(ctx, session.Request{Descriptor: desc}, borrow)
	if err != nil {
		return nil, fmt.Errorf("session.select: driver.connect failed for %s/%s: %w", input.Platform, input.ID, err)
	}
	active, err := s.install(ctx, sess, input.Platform, input.ID, desc)
	if err != nil {
		return nil, err
	}
	s.logger.Info("session.device_selected", logging.LogContext{
		"platform": string(input.Platform), "id": input.ID, "kind": string(active.Kind),
	})
	return active, nil
}

// checkPinned fails with DEVICE_PINNED when the session is bound to a
// device and input names another one. An iOS id compares without
// regard to case, as the scan that resolves it does.
func (s *DeviceSession) checkPinned(input SelectDeviceInput) error {
	if s.pinned == nil {
		return nil
	}
	sameID := input.ID == s.pinned.ID
	if input.Platform == device.PlatformIOS {
		sameID = strings.EqualFold(input.ID, s.pinned.ID)
	}
	if input.Platform == s.pinned.Platform && sameID {
		return nil
	}
	return diag.New(
		diag.CodeDevicePinned,
		fmt.Sprintf("This server is bound to device %s and cannot select device %s on %s.",
			s.pinned.ID, input.ID, input.Platform),
		"This server is bound to one device. Keep using it, or ask the operator to start a server for the other device.",
		nil,
	)
}

// selectPinned selects the pinned device when nothing was ever selected
// and the session is still open. A session without a pin, or one that
// already selected, is left alone.
func (s *DeviceSession) selectPinned(ctx context.Context) error {
	if s.pinned == nil {
		return nil
	}
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	s.mu.Lock()
	needed := !s.closed && s.active == nil && s.remembered == nil
	s.mu.Unlock()
	if !needed {
		return nil
	}
	_, err := s.selectDevice(ctx, *s.pinned, false)
	return err
}

// rememberLaunch stores the launched-app state of a session this one gave
// up on purpose, so a later re-acquire can offer it to the next driver.
func (s *DeviceSession) rememberLaunch(previous *session.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous != nil && s.remembered != nil {
		s.remembered.launch = previous.LaunchState()
	}
}

// Ensure returns the bound device, taking the remembered device again
// first when the session released or lost it. The re-acquire runs
// outside the tool call's time budget, and its runner build runs
// outside acquireMu. A build that finishes after another call already
// took the device, or after a selection replaced the remembered device,
// is not used: the first case returns the held device and the second
// starts over for the new descriptor. A device another process now
// holds fails with DEVICE_RELEASED_HELD. With no device ever selected it
// fails with the no-active-device error. A canceled ctx returns its own
// error.
func (s *DeviceSession) Ensure(ctx context.Context) (*ActiveDevice, error) {
	if err := s.selectPinned(ctx); err != nil {
		return nil, err
	}
	defer pauseToolBudget(ctx)()
	ctx, stop := s.withLife(ctx)
	defer stop()
	for {
		active, remembered, err := s.heldOrRemembered()
		if active != nil || err != nil {
			return active, err
		}
		if err := s.prepare(ctx, remembered.desc); err != nil {
			return nil, err
		}
		active, retry, err := s.reacquire(ctx, remembered)
		if retry {
			continue
		}
		return active, err
	}
}

// heldOrRemembered snapshots what Ensure needs: the bound device, or
// else the remembered one to take again. A session that handed its device
// to a borrower may not take it again until it stops following that
// borrower, because a call queued behind the yield would otherwise win
// the device back before the borrower acquires it.
func (s *DeviceSession) heldOrRemembered() (*ActiveDevice, *rememberedDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return nil, nil, errSessionClosed
	case s.active != nil:
		return s.active, nil, nil
	case s.remembered == nil:
		return nil, nil, errNoActiveDevice
	case s.state == holderBorrowed:
		return nil, nil, diag.NewDeviceReleasedHeld(s.borrowerLabelLocked())
	}
	return nil, s.remembered, nil
}

// reacquire opens the remembered device under acquireMu once its
// runner is prepared. retry is true when a selection replaced the
// remembered device while it prepared.
func (s *DeviceSession) reacquire(ctx context.Context, prepared *rememberedDevice) (active *ActiveDevice, retry bool, err error) {
	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()
	current, remembered, err := s.heldOrRemembered()
	if current != nil || err != nil {
		return current, false, err
	}
	if remembered != prepared {
		return nil, true, nil
	}
	req := session.Request{Descriptor: remembered.desc, Reacquire: true}
	if remembered.launch != (session.LaunchState{}) {
		launch := remembered.launch
		req.Restore = &launch
	}
	sess, err := s.open(ctx, req, false)
	if err != nil {
		return nil, false, err
	}
	active, err = s.install(ctx, sess, remembered.desc.Platform, remembered.desc.ID, remembered.desc)
	if err != nil {
		return nil, false, err
	}
	s.logger.Info("session.device_reacquired", logging.LogContext{
		"platform": string(active.Platform), "id": active.ID,
	})
	if sess.Relaunched() {
		addNotice(ctx, appRelaunchedNotice)
	}
	return active, false, nil
}

// withLife returns a context that ends when ctx ends or when the
// session closes, whichever comes first. A close cancels it with
// errSessionClosed as the cause.
func (s *DeviceSession) withLife(ctx context.Context) (context.Context, func()) {
	joined, cancel := context.WithCancelCause(ctx)
	detach := context.AfterFunc(s.life, func() { cancel(context.Cause(s.life)) })
	return joined, func() {
		detach()
		cancel(context.Canceled)
	}
}

// prepare runs the lease-free runner build for desc. It must run
// without acquireMu, because the build can take minutes.
func (s *DeviceSession) prepare(ctx context.Context, desc device.Descriptor) error {
	reportPhase(ctx, phaseBuilding)
	return canceledOr(ctx, session.Prepare(ctx, s.deps, desc, s.timeouts.RunnerBuildMs))
}

// open opens the prepared device within the acquisition timeout. Phases
// are reported to the calling client, except the connected phase, which
// install reports once the session is installed. The caller holds acquireMu.
//
// With borrow set, a device held by an idle process that can yield is
// taken from it. A request that re-acquires a remembered device never
// borrows, whatever borrow says, so two idle sessions cannot pass the
// device back and forth.
func (s *DeviceSession) open(ctx context.Context, req session.Request, borrow bool) (*session.Session, error) {
	req.Gate = s.gate
	req.Yielder = s
	acq := session.Acquisition{
		BudgetMs: s.timeouts.DeviceAcquireMs,
		Clock:    s.timers,
		Progress: func(p session.Phase) {
			if p != session.PhaseConnected {
				reportPhase(ctx, acquirePhaseFor(p))
			}
		},
	}
	var sess *session.Session
	var err error
	if borrow && !req.Reacquire {
		sess, err = session.BorrowWithinBudget(ctx, s.deps, req, acq, selectBorrower)
	} else {
		sess, err = session.OpenWithinBudget(ctx, s.deps, req, acq)
	}
	return sess, canceledOr(ctx, err)
}

// canceledOr returns the cancellation cause when ctx ended, because an
// acquisition that a canceled caller or a closing session killed fails
// with whatever the killed child reported, such as an unreachable agent,
// which would mislead the caller.
func canceledOr(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func acquirePhaseFor(p session.Phase) acquirePhase {
	switch p {
	case session.PhaseBuilding:
		return phaseBuilding
	case session.PhaseLaunching:
		return phaseLaunching
	}
	return phaseConnected
}

// install makes sess the session's bound device and releases a session
// a concurrent selection installed first instead of orphaning it. A
// session closed in the meantime releases sess instead.
func (s *DeviceSession) install(ctx context.Context, sess *session.Session, platform device.Platform, id string, desc device.Descriptor) (*ActiveDevice, error) {
	built := sess.Built()
	kind := selectKindFromDeviceKind(desc.Kind)
	if info, infoErr := built.Orchestra.DeviceInfo(ctx); infoErr == nil {
		kind = selectKindFromDriverKind(info.Kind)
	}
	active := &ActiveDevice{
		Platform: platform, ID: id, Kind: kind,
		Orchestra: built.Orchestra, Driver: built.Driver, ConnectedAt: s.clock.NowMs(),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = sess.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
		return nil, errSessionClosed
	}
	displaced := s.sess
	s.sess = sess
	s.active = active
	s.remembered = &rememberedDevice{desc: desc}
	s.bindRecorder(sess)
	s.stopBorrowViewLocked()
	s.state = holderRunning
	s.settleLocked() //nolint:contextcheck // idle timers outlive any call, so they take no call context
	s.mu.Unlock()
	s.releasePrevious(displaced) //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	go s.watchLost(sess)         //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	reportPhase(ctx, phaseConnected)
	return active, nil
}

// detach removes the bound session and returns it for the caller to
// close. forget also drops the remembered descriptor, which a disconnect
// does. Without it the descriptor stays remembered and the state is
// released, so a later Ensure can take the device again.
func (s *DeviceSession) detach(forget bool) *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.sess
	s.sess = nil
	s.active = nil
	s.cancelTimerLocked()
	s.stopBorrowViewLocked()
	if forget {
		s.remembered = nil
		s.state = holderUnselected
	} else {
		s.state = holderReleased
		s.releasedBy = releasedIdle
	}
	return prev
}

// releasePrevious closes a session the DeviceSession no longer tracks.
// A nil previous is a no-op, and a close failure is logged because the
// replacement selection must proceed regardless.
func (s *DeviceSession) releasePrevious(previous *session.Session) {
	if previous == nil {
		return
	}
	desc := previous.Descriptor()
	err := previous.Close()
	s.carryInterventions(previous)
	if err != nil {
		s.logger.Warn("session.previous_disconnect_failed", logging.LogContext{
			"id": desc.ID, "platform": string(desc.Platform), "error": err.Error(),
		})
		return
	}
	s.logger.Info("session.previous_disconnected", logging.LogContext{
		"id": desc.ID, "platform": string(desc.Platform),
	})
}

// EnterCall admits a tool call that drives the device through the
// activity gate, so a fresh capture of the held session never runs
// between two of the call's device operations. The returned function
// ends the admission and must run when the call ends. It fails with the
// context error when ctx ends while a fresh capture holds the gate.
func (s *DeviceSession) EnterCall(ctx context.Context) (func(), error) {
	return s.gate.Enter(ctx)
}

// BeginCall records that a tool call starts. It stops the idle timers,
// marks the held device running, and reports RUN_EXPIRED when the call
// continues a run the open-run idle timeout already closed. A call that
// returns an error never started and must not be matched by EndCall.
func (s *DeviceSession) BeginCall(tool string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := s.expiryErrorLocked(tool); err != nil {
		return err
	}
	if toolsNeedingTheDevice[tool] && s.pausedLocked() {
		return diag.NewHolderBusy("a person is debugging a run on this device")
	}
	if tool == "start_run" {
		s.expired = nil
	}
	s.inFlight++
	s.cancelTimerLocked()
	if s.sess != nil {
		s.state = holderRunning
		_ = s.sess.SetRunning(s.openRunID())
	}
	return nil
}

// EndCall records that a tool call ended. When it was the last call in
// flight, the idle timer for the current state starts from now.
func (s *DeviceSession) EndCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	s.settleLocked()
}

// expiryErrorLocked reports RUN_EXPIRED for a call on a run the
// open-run idle timeout closed. A run-scoped tool always reports it
// until a new run starts. The first device-needing tool reports it
// once, and later ones follow the released path instead.
func (s *DeviceSession) expiryErrorLocked(tool string) error {
	if s.expired == nil {
		return nil
	}
	if toolsOnTheRun[tool] {
		s.expired.notified = true
		return diag.NewRunExpired(s.expired.id)
	}
	if toolsNeedingTheDevice[tool] && !s.expired.notified {
		s.expired.notified = true
		return diag.NewRunExpired(s.expired.id)
	}
	return nil
}

// pausedLocked reports that the held session's run is stopped for a
// person to debug. A paused session is owned by that person, so no idle
// timer may release it and no other process may take it.
func (s *DeviceSession) pausedLocked() bool {
	if s.sess == nil {
		return false
	}
	_, paused := s.sess.Paused()
	return paused
}

func (s *DeviceSession) openRunID() string {
	if s.runs == nil {
		return ""
	}
	return s.runs.OpenRunID()
}

// settleLocked moves a held device with no call in flight to idle, or to
// running when a run is open, records that in the lease metadata, and
// arms the timer that governs that state.
func (s *DeviceSession) settleLocked() {
	if s.closed || s.sess == nil || s.inFlight > 0 {
		return
	}
	if runID := s.openRunID(); runID != "" {
		s.state = holderRunning
		_ = s.sess.SetRunning(runID)
		s.armTimerLocked(timerOpenRun, s.timeouts.OpenRunIdleMs)
		return
	}
	s.state = holderIdle
	_ = s.sess.SetIdle()
	s.armTimerLocked(timerIdle, s.timeouts.McpIdleMs)
}

func (s *DeviceSession) cancelTimerLocked() {
	if s.timerStop != nil {
		s.timerStop()
		s.timerStop = nil
	}
	s.timerGen++
}

// armTimerLocked starts a timer of ms milliseconds on the session
// clock. It replaces any earlier one, and a timer that fires after a
// later call replaced it does nothing.
func (s *DeviceSession) armTimerLocked(kind timerKind, ms int64) {
	s.cancelTimerLocked()
	timerCtx, cancel := context.WithCancel(s.timerBase)
	s.timerStop = cancel
	gen := s.timerGen
	go func() {
		if s.timers.Sleep(timerCtx, ms) == nil {
			s.onTimer(gen, kind) //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
		}
	}()
}

// onTimer gives the device up when its idle timer fired and nothing
// happened since. An idle timeout releases the device quietly. An
// open-run timeout also closes the open run as cancelled and records it,
// so the agent's next call on that run reports RUN_EXPIRED.
func (s *DeviceSession) onTimer(gen uint64, kind timerKind) {
	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()

	s.mu.Lock()
	if s.closed || gen != s.timerGen || s.inFlight > 0 || s.sess == nil {
		s.mu.Unlock()
		return
	}
	if s.pausedLocked() {
		s.settleLocked()
		s.mu.Unlock()
		return
	}
	sess := s.sess
	remembered := s.remembered
	s.sess = nil
	s.active = nil
	s.state = holderReleased
	s.releasedBy = releasedIdle
	s.cancelTimerLocked()
	s.mu.Unlock()

	if kind == timerOpenRun && s.runs != nil {
		id := s.runs.CloseOpenRun(runfolder.OutcomeCancelled,
			"closed because the session sat idle past the open-run idle timeout and released the device")
		if id != "" {
			s.mu.Lock()
			s.expired = &expiredRun{id: id}
			s.mu.Unlock()
		}
	}
	s.closeAndRemember(context.Background(), sess, remembered, "session.released")
}

// closeAndRemember closes sess and stores the launched-app state it
// carried, so a re-acquire can offer it to the next driver.
func (s *DeviceSession) closeAndRemember(ctx context.Context, sess *session.Session, remembered *rememberedDevice, event string) {
	desc := sess.Descriptor()
	err := sess.CloseContext(ctx)
	s.carryInterventions(sess)
	if err != nil {
		s.logger.Warn("session.release_failed", logging.LogContext{"id": desc.ID, "error": err.Error()})
	}
	if remembered != nil {
		s.mu.Lock()
		remembered.launch = sess.LaunchState()
		s.mu.Unlock()
	}
	s.logger.Info(event, logging.LogContext{"id": desc.ID, "platform": string(desc.Platform)})
}

// watchLost moves the session to lost when sess loses its lease without
// the session choosing to. It ends when sess closes by any other path.
func (s *DeviceSession) watchLost(sess *session.Session) {
	select {
	case <-sess.Lost():
	case <-sess.Closed():
		return
	}
	s.acquireMu.Lock()
	defer s.acquireMu.Unlock()

	s.mu.Lock()
	if s.sess != sess {
		s.mu.Unlock()
		return
	}
	remembered := s.remembered
	s.sess = nil
	s.active = nil
	s.state = holderLost
	s.cancelTimerLocked()
	s.mu.Unlock()

	if s.runs != nil {
		s.runs.CancelScriptsForLostDevice(sess.LostErr())
		s.runs.CloseOpenRun(runfolder.OutcomeDeviceLost, "closed because the device was lost")
	}
	s.closeAndRemember(context.Background(), sess, remembered, "session.lost")
}

// CheckHealth reports the session state and, when a device is bound,
// whether its agent answers a bounded liveness probe. It never returns
// an error and never takes a device: no device ever selected is a
// normal report with Selected false, a released or lost device is
// reported by its state without being touched, and an unresponsive
// agent is a normal report with Healthy false, since a failed probe is a
// check outcome, not a failure of the check itself.
func (s *DeviceSession) CheckHealth(ctx context.Context) orchestra.HealthReport {
	s.mu.Lock()
	active, state, remembered, releasedBy := s.active, s.state, s.remembered, s.releasedBy
	s.mu.Unlock()
	switch {
	case active != nil:
		report := active.Orchestra.CheckHealth(ctx)
		report.State = string(state)
		return report
	case remembered == nil:
		return orchestra.HealthReport{Selected: false, State: string(holderUnselected)}
	case state == holderLost:
		return orchestra.HealthReport{
			Selected: true, State: string(state),
			Message: "the session lost its device and has not probed it",
			Remedy:  "Call a device tool to take the device again, or select_device to pick another one.",
		}
	}
	if state == holderBorrowed {
		return orchestra.HealthReport{
			Selected: true, State: string(state),
			Message: "the session handed its device to another process and has not probed it",
			Remedy:  "Wait for that process to finish, then call a device tool to take the device again.",
		}
	}
	message := "the session released its device after an idle timeout and has not probed it"
	if releasedBy == releasedHandedOver {
		message = "the session handed its device to another process that no longer holds it and has not probed it"
	}
	return orchestra.HealthReport{
		Selected: true, State: string(holderReleased),
		Message: message,
		Remedy:  "Call a device tool to take the device again.",
	}
}

// Close finalizes the open run as cancelled, cancels the scripts that
// are their own run, disconnects the active driver, waits for the close
// of a session handed to a borrower, and makes every
// later Select release what it builds and fail, so a selection that was
// still launching when the server stopped cannot leave a child running.
// It is idempotent.
func (s *DeviceSession) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancelLife(errSessionClosed)
	if s.runs != nil {
		s.runs.Close()
	}
	_ = s.Disconnect(context.Background())
	s.yields.Wait()
}

func (s *DeviceSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Disconnect tears down the active driver, forgets the device and marks
// the session unselected. It is a no-op when the session is already
// unselected. It waits for a transition in flight only until ctx ends,
// then returns the context error and changes nothing.
func (s *DeviceSession) Disconnect(ctx context.Context) error {
	if err := s.acquireMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.acquireMu.Unlock()
	previous := s.detach(true)
	if previous == nil {
		return nil
	}
	desc := previous.Descriptor()
	err := previous.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	s.carryInterventions(previous)
	if err != nil {
		s.logger.Warn("session.disconnect_failed", logging.LogContext{
			"id": desc.ID, "platform": string(desc.Platform), "error": err.Error(),
		})
		return nil
	}
	s.logger.Info("session.disconnected", logging.LogContext{
		"id": desc.ID, "platform": string(desc.Platform),
	})
	return nil
}

// resolveDescriptor builds the device.Descriptor Factory builds from
// by resolving the platform and id through device.ResolveDescriptor,
// the same path the CLI uses, so the device kind comes from the
// device's identity as the probe sees it. input.Kind is a legacy hint
// that is accepted on the wire and deliberately not consulted. An iOS
// id no scan finds is reported as DEVICE_NOT_FOUND with a remedy.
func (s *DeviceSession) resolveDescriptor(ctx context.Context, input SelectDeviceInput) (device.Descriptor, error) {
	desc, err := device.ResolveDescriptor(ctx, s.probe, s.runners, input.Platform, input.ID)
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

// PublishStep hands a step line to the viewers of the held session. It
// does nothing while no device is held.
func (s *DeviceSession) PublishStep(line runfolder.StepLine) {
	s.mu.Lock()
	held := s.sess
	s.mu.Unlock()
	if held != nil {
		held.PublishStep(line)
	}
}

// SetInterventionRecorder routes the interventions of the held device to
// recorder, which a run sets when it opens and clears with nil when it
// ends. A session installed later receives the same recorder, so a run
// opened before a device was selected still keeps what happens to it.
func (s *DeviceSession) SetInterventionRecorder(recorder session.InterventionRecorder) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	s.recorder = recorder
	if s.recSess != nil {
		s.recSess.SetInterventionRecorder(recorder)
	}
}

// bindRecorder hands the current recorder to a newly installed session.
func (s *DeviceSession) bindRecorder(sess *session.Session) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	s.recSess = sess
	sess.SetInterventionRecorder(s.recorder)
}

// Interventions implements session.InterventionSource. It returns what
// happened to the held session and to the sessions given up before it,
// oldest first, and clears both. It never takes a device, so a call that
// is not session activity may drain it.
func (s *DeviceSession) Interventions() []session.Intervention {
	s.pendMu.Lock()
	drained := s.carried
	s.carried = nil
	s.pendMu.Unlock()

	s.mu.Lock()
	held := s.sess
	s.mu.Unlock()
	if held != nil {
		drained = append(drained, held.Interventions()...)
	}
	sort.SliceStable(drained, func(i, j int) bool { return drained[i].AtMs < drained[j].AtMs })
	return drained
}

// carryInterventions moves the pending entries of a session this host is
// giving up into carried. It runs after the session closed, so entries
// the close itself recorded are kept too.
func (s *DeviceSession) carryInterventions(gone *session.Session) {
	entries := gone.Interventions()
	if len(entries) == 0 {
		return
	}
	s.pendMu.Lock()
	defer s.pendMu.Unlock()
	s.carried = append(s.carried, entries...)
	if over := len(s.carried) - maxCarriedInterventions; over > 0 {
		s.carried = s.carried[over:]
	}
}

var _ session.InterventionSource = (*DeviceSession)(nil)
