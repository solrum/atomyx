package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
)

// IdentityResolver resolves a descriptor to the canonical identity a
// lease keys on, without touching the device.
type IdentityResolver interface {
	Resolve(ctx context.Context, d device.Descriptor) (device.Identity, error)
}

// Deps configures Open.
type Deps struct {
	// Factory builds the driver once the lease is held. Required. It must
	// be a device.WrappingFactory so every session's driver is built
	// inside the observing decorator that feeds its viewers.
	Factory device.WrappingFactory
	// Identity resolves the canonical identity. Nil keys the lease on the
	// descriptor's own platform and id, which suits a host whose device
	// ids are already canonical, such as an iOS udid.
	Identity IdentityResolver
	// Leases arbitrates which process drives a device. Nil disables
	// exclusion entirely, which only a single-process host such as a test
	// wants. Every production composition root sets it.
	Leases LeaseAcquirer
	// Sockets says where the holder of each device serves viewers. Nil
	// binds no attach socket, and so does a nil Leases, because a session
	// without a lease has no exclusive claim on the path. It is a separate
	// dependency from Leases, so wrapping the acquirer never drops the
	// socket silently. Every production composition root sets it to the
	// same lease manager it sets as Leases.
	Sockets SocketNamer
	// Preparer does the lease-free work Prepare runs before Open, such as
	// building the iOS runner. Nil makes Prepare a no-op.
	Preparer device.RunnerPreparer
	// Clock times the lease watcher. Defaults to clock.SystemClock.
	Clock clock.Clock
	// Logger receives acquisition and reaping lines. Defaults to a
	// no-op logger.
	Logger logging.Logger
	// Command and Role describe this process in the lease metadata other
	// processes read when they find the device held, for example
	// "atomyx run" and "cli".
	Command string
	Role    string
	// PID defaults to the running process.
	PID int
	// Owners identifies the process that holds a device Borrow asks to
	// yield, so the runner it leaves behind is handed back to it. Nil
	// borrows without naming a lender, and the runner is then stopped when
	// the borrower exits.
	Owners OwnerResolver
	// RequestYield asks a holder to give up its device. Nil selects
	// attach.RequestYield.
	RequestYield YieldRequester
}

// Request names the device to open and how.
type Request struct {
	Descriptor device.Descriptor
	// Reacquire states that the caller held this device before and gave
	// it up. A lease conflict then reports DEVICE_RELEASED_HELD, whose
	// remedy addresses an agent, instead of DEVICE_HELD, whose remedy
	// addresses an operator.
	Reacquire bool
	// Restore carries launched-app state from an earlier session on this
	// device. It applies only when the rebuilt driver proves it reached
	// the same runner instance.
	Restore *LaunchState
	// Gate is the activity gate the session's FreshTree takes. A host
	// that outlives its sessions passes one gate to every Open, so a
	// tool call in flight keeps excluding fresh captures across a
	// re-acquire. Nil gives the session its own gate.
	Gate *ActivityGate
	// SocketRefresher answers a viewer's fresh capture request on the
	// session's attach socket. Nil selects the session's own capture,
	// which the holder state admits only while the session is idle. A host
	// that manages the holder state itself, such as the inspector, sets
	// its own, so a capture it already guards is not refused by the
	// session's state check, and an idle release cannot close the session
	// mid-capture.
	SocketRefresher Refresher
	// Yielder answers a control.yield request on the session's attach
	// socket. Nil means the session cannot yield, so its hello omits the
	// control feature.
	Yielder Yielder
	// Debugger controls a paused run on the session's behalf. Nil means
	// the session cannot be debugged.
	Debugger Debugger
	// OnLastDebugViewerLeft is called when the last viewer that sent a
	// debug or act request ends its connection. It runs on that
	// connection's goroutine, so it must return promptly. Nil means the
	// host does not care.
	OnLastDebugViewerLeft func()
	// Lender names the process that lent the device to this open. A runner
	// the driver factory adopts records it, and the opener's exit hands the
	// runner back to it while it lives. The zero value means nobody lent
	// the device.
	Lender device.RunnerOwner
}

// LaunchState is the launched-app state of an iOS driver, carried
// across a release and the re-acquire that follows.
type LaunchState struct {
	BundleID string
	RunnerID string
}

// launchedAppCarrier is implemented by a driver that tracks the app it
// launched. The iOS drivers implement it.
type launchedAppCarrier interface {
	LaunchedApp() (bundleID, runnerID string)
	RestoreLaunchedApp(ctx context.Context, bundleID, fromRunnerID string) error
}

// ErrNoFactory reports an Open whose Deps carry no device factory.
var ErrNoFactory = errors.New("session deps carry no device factory")

// observingFactory makes factory build every driver inside an observing
// decorator that publishes to hub, so the Orchestra, the runner, the
// tool handlers and failure capture all act through it.
func observingFactory(factory device.WrappingFactory, hub *Hub) device.Factory {
	return factory.WithDriverWrap(func(d driver.Driver) driver.Driver {
		return NewObservingDriver(d, hub)
	})
}

// Open takes ownership of the device named by req.
//
// It resolves the canonical identity, acquires the device lease, and
// builds the driver, which is where an iOS runner left by an earlier
// holder is adopted or ended. A lease held by another process fails with DEVICE_HELD,
// or DEVICE_RELEASED_HELD for a Reacquire, before any driver is built.
// An identity failure, such as an unauthorized Android device, fails
// with a coded error before the lease is taken. On any failure nothing
// is left held.
//
// Once the driver is built, a leased session with Deps.Sockets set binds
// its attach socket, replacing a stale file at that path, and records the
// path in the lease metadata. A socket that cannot be bound is logged and
// the session proceeds without one. A leased session with no Deps.Sockets
// records the reason it serves no socket, so a process that wants to
// attach learns it at once.
func Open(ctx context.Context, deps Deps, req Request) (*Session, error) {
	deps = deps.withDefaults()
	desc := req.Descriptor

	if deps.Factory == nil {
		return nil, ErrNoFactory
	}
	hub := NewHub()
	factory := observingFactory(deps.Factory, hub)

	identity, err := deps.ResolveIdentity(ctx, desc)
	if err != nil {
		return nil, err
	}

	var held *lease.Lease
	var key lease.Key
	if deps.Leases != nil {
		key = lease.KeyFor(string(identity.Platform), identity.HardwareID)
		held, err = deps.Leases.Acquire(ctx, key, lease.Holder{
			PID:              deps.PID,
			Command:          deps.Command,
			Role:             deps.Role,
			StartedAt:        time.UnixMilli(deps.Clock.NowMs()).UTC(),
			ProcessStartedAt: deps.processStart(),
		})
		if err != nil {
			return nil, mapLeaseError(err, req.Reacquire)
		}
	}

	ctx = logging.WithDevice(ctx, logging.DeviceTag{DeviceID: desc.ID, HardwareID: identity.HardwareID})
	built, err := factory.Build(device.WithLender(ctx, req.Lender), desc)
	if err != nil {
		if held != nil {
			_ = held.Release()
		}
		return nil, err
	}
	relaunched := false
	if req.Restore != nil {
		if carrier, ok := driver.As[launchedAppCarrier](built.Driver); ok {
			if err := carrier.RestoreLaunchedApp(ctx, req.Restore.BundleID, req.Restore.RunnerID); err != nil {
				deps.Logger.Warn("could not restore the launched app after re-acquiring the device", logging.LogContext{"id": desc.ID, "error": err.Error()})
			} else {
				relaunched = relaunchedApp(carrier, *req.Restore)
			}
		}
	}
	gate := req.Gate
	if gate == nil {
		gate = NewActivityGate()
	}
	s := newSession(ctx, deps, desc, identity, held, built, hub, gate)
	s.relaunched = relaunched
	s.socketRefresher = req.SocketRefresher
	s.yielder = req.Yielder
	s.debugger = req.Debugger
	s.onLastDebugViewerLeft = req.OnLastDebugViewerLeft
	if held != nil {
		if deps.Sockets == nil {
			s.socketError = reasonNoSocketConfigured
		} else {
			s.bindAttach(deps.Sockets.SocketPath(key))
		}
	}
	if err := s.persistActivity(); err != nil {
		_ = s.CloseContext(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("record the holder state: %w", err)
	}
	return s, nil
}

// relaunchedApp reports whether restoring state launched its app afresh.
// A driver that connected to the runner instance that launched the app
// only resumes tracking it. Any other runner has to launch the app, which
// discards the app's in-memory state.
func relaunchedApp(carrier launchedAppCarrier, restore LaunchState) bool {
	if restore.BundleID == "" {
		return false
	}
	_, connectedRunnerID := carrier.LaunchedApp()
	return restore.RunnerID == "" || connectedRunnerID != restore.RunnerID
}

// processStart reads when this process started, or nil when Owners is
// unset or cannot identify it. A holder without it can still be asked to
// yield, but a borrower will not record it as the lender of a runner.
func (d Deps) processStart() *time.Time {
	if d.Owners == nil {
		return nil
	}
	self, err := d.Owners.ResolveOwner(d.PID)
	if err != nil {
		d.Logger.Warn("could not read the start time of this process for the lease", logging.LogContext{"pid": d.PID, "error": err.Error()})
		return nil
	}
	started := self.StartTime.UTC()
	return &started
}

func (d Deps) withDefaults() Deps {
	if d.Clock == nil {
		d.Clock = clock.SystemClock{}
	}
	if d.Logger == nil {
		d.Logger = logging.NoopLogger{}
	}
	if d.PID == 0 {
		d.PID = os.Getpid()
	}
	return d
}

// ResolveIdentity reports the canonical identity Open keys the lease
// on for desc, as a coded error when the device cannot be identified.
// A host compares it with the identity of a session it holds to tell
// whether two descriptors reach one device.
func (d Deps) ResolveIdentity(ctx context.Context, desc device.Descriptor) (device.Identity, error) {
	if d.Identity == nil {
		return device.Identity{Platform: desc.Platform, HardwareID: desc.ID}, nil
	}
	identity, err := d.Identity.Resolve(ctx, desc)
	if err != nil {
		return device.Identity{}, mapIdentityError(err)
	}
	return identity, nil
}

// mapIdentityError turns the typed identity errors of the device
// package, which does not know diag codes, into coded diagnostics.
func mapIdentityError(err error) error {
	var state *device.DeviceStateError
	if errors.As(err, &state) {
		return diag.New(diag.CodeAgentUnreachable,
			fmt.Sprintf("device %s is %s", state.ID, state.State),
			stateRemedy(state.State), err)
	}
	var unresolved *device.IdentityUnresolvedError
	if errors.As(err, &unresolved) {
		return diag.New(diag.CodeDeviceNotFound,
			fmt.Sprintf("device %s reports no usable hardware identity", unresolved.ID),
			"Confirm the device or emulator has finished booting, then try again.", err)
	}
	return err
}

func stateRemedy(state device.State) string {
	switch state {
	case device.StateUnauthorized:
		return "Unlock the device and accept the USB debugging authorization prompt, then try again."
	case device.StateOffline:
		return "Reconnect the device or restart the emulator, then try again."
	default:
		return "Wait for the device to finish starting, or reconnect it, then try again."
	}
}

// mapLeaseError converts a lease conflict into the code the caller's
// audience needs and passes every other error through with context.
func mapLeaseError(err error, reacquire bool) error {
	var heldErr *lease.HeldError
	if errors.As(err, &heldErr) {
		if reacquire {
			return diag.NewDeviceReleasedHeld(heldErr.Holder.Describe())
		}
		return err
	}
	return fmt.Errorf("acquire the device lease: %w", err)
}
