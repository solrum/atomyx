package inspector

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

// ErrBorrowed is returned by a Host's methods in the moment between
// giving the device to another process and watching that process. It
// reports the same code as ErrReleased, because the page treats both as a
// device it does not have and offers the same reconnect.
var ErrBorrowed = fmt.Errorf("inspector: the device was handed to another process: %w", ErrReleased)

// interventionRecorder is implemented by a target that keeps the
// interventions other processes cause, which *session.Session does.
type interventionRecorder interface {
	RecordIntervention(kind, client string)
}

var _ session.Yielder = (*Host)(nil)

// contextCloser is implemented by a target whose close takes a context for
// its values, which *session.Session does.
type contextCloser interface {
	CloseContext(ctx context.Context) error
}

func closeWithin(ctx context.Context, t Target) error {
	if c, ok := t.(contextCloser); ok {
		return c.CloseContext(ctx)
	}
	return t.Close()
}

// Yield implements session.Yielder. It accepts only while the host
// holds the device and no operator action is driving it, because a
// capture or refresh in flight owns the device.
//
// The host's operation lock is taken without waiting and kept until the
// session is closed, so a release or a reconnect never interleaves with
// the hand-over. Waiting for it would deadlock, because an idle release
// holds it while it closes the session, and that close waits for the
// connection goroutine this call runs on.
func (h *Host) Yield(ctx context.Context, borrower string) (func(), error) {
	if !h.opMu.TryLock() {
		return nil, diag.NewHolderBusy("the inspector is changing its device")
	}
	held, ok := h.held()
	if !ok {
		h.opMu.Unlock()
		return nil, diag.NewHolderBusy("the inspector holds no device at the moment")
	}
	if _, owns := held.(Lifecycle); !owns {
		h.opMu.Unlock()
		return nil, diag.NewHolderBusy("the inspector only watches the device")
	}
	info := held.Info()
	followCtx, stopFollow := context.WithCancel(context.WithoutCancel(ctx))
	h.mu.Lock()
	if h.driving > 0 {
		stopFollow()
		h.mu.Unlock()
		h.opMu.Unlock()
		return nil, diag.NewHolderBusy("a capture or an action is running")
	}
	h.dropLocked(ErrBorrowed, info)
	h.stopFollow = stopFollow
	h.mu.Unlock()

	if rec, ok := held.(interventionRecorder); ok {
		rec.RecordIntervention(session.InterventionControlBorrowed, borrower)
	}
	return func() {
		err := closeWithin(followCtx, held)
		h.opMu.Unlock()
		if err != nil {
			h.logger.Warn("inspector.yield.close_failed", logging.LogContext{"error": err.Error()})
		}
		h.logger.Info("inspector.yielded", nil)
		h.followBorrower(followCtx, stopFollow)
	}, nil
}

// followBorrower serves a view of the process that took the device, so
// the page shows its state and its steps and drops to the released
// banner when it ends. The host answers released when no follower is
// configured, when none takes the device in time, and when the watch
// fails. A reconnect that adopted a device meanwhile wins. The watchers of an
// adopted borrower live under ctx, which Close ends.
func (h *Host) followBorrower(ctx context.Context, stop context.CancelFunc) {
	adopted := false
	defer func() {
		if !adopted {
			stop()
		}
	}()
	var target Target
	var err error
	if h.follow != nil {
		target, err = h.follow(ctx)
	} else {
		err = session.ErrNoBorrower
	}
	var info session.Info
	if err == nil {
		info = target.Info()
	}
	h.opMu.Lock()
	defer h.opMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil || !errors.Is(h.gone, ErrBorrowed) || ctx.Err() != nil {
		if target != nil {
			defer func() { _ = target.Close() }()
		}
		return
	}
	if err != nil {
		h.gone = ErrReleased
		return
	}
	h.adoptLocked(ctx, target, info)
	adopted = true
}
