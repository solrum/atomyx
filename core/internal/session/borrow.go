package session

import (
	"context"
	"errors"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/shutdown"
)

const (
	// YieldBudgetMs bounds a borrow after it decides to ask: the holder's
	// reply, its close, and the borrower's retries for the lease. The
	// holder's close is bounded by the shutdown grace period, so the
	// budget is that period plus a margin for the reply and for the
	// borrower's own retry pacing.
	YieldBudgetMs int64 = int64(shutdown.GracePeriod/time.Millisecond) + yieldMarginMs

	yieldMarginMs int64 = 5_000
	borrowPollMs  int64 = 50
)

// errYieldBudget is the cancellation cause of a borrow whose yield
// budget elapsed.
var errYieldBudget = errors.New("the holder did not release the device in time")

// OwnerResolver identifies a live process as a runner owner.
// device.OwnerResolver satisfies it.
type OwnerResolver interface {
	ResolveOwner(pid int) (device.RunnerOwner, error)
}

// YieldRequester asks the holder serving the socket named by cfg to give
// up its device to the process described by command.
// attach.RequestYield satisfies it.
type YieldRequester func(ctx context.Context, cfg attach.ClientConfig, command string) error

// Borrow acquires the device like Acquire and, when another process
// holds it, asks that holder to yield and takes the device from it.
//
// The device is prepared first, so no runner build happens while the
// holder is gone. See BorrowWithinBudget for the rest of the contract.
func Borrow(ctx context.Context, deps Deps, req Request, acq Acquisition, borrower string) (*Session, error) {
	if acq.Progress != nil {
		acq.Progress(PhaseBuilding)
	}
	if err := Prepare(ctx, deps, req.Descriptor, acq.BuildBudgetMs); err != nil {
		return nil, err
	}
	return BorrowWithinBudget(ctx, deps, req, acq, borrower)
}

// BorrowWithinBudget is the open half of Borrow for a host that already
// ran Prepare itself, so the build neither repeats nor runs under a lock
// the open must hold. It reports PhaseLaunching once and PhaseConnected,
// and never PhaseBuilding.
//
// Only a plain acquisition that fails with DEVICE_HELD leads to a
// yield. A request with Reacquire fails with
// DEVICE_RELEASED_HELD instead, so it never borrows.
// borrower is the short command string the holder shows to its own user,
// for example "atomyx run login.yml".
//
// The original DEVICE_HELD error, naming the holder, is returned
// unchanged when the holder serves no socket, does not advertise that it
// can yield, refuses because it is busy, cannot be reached, or still
// holds the device when YieldBudgetMs ends. When another process takes
// the device between the holder's release and this acquisition, the
// error names that process instead. A connection that ends after the
// request was sent counts as a yield that may have happened, so the
// acquisition still goes ahead.
//
// The holder is recorded as the lender of the new session, so a runner
// left running by the holder is handed back to it when this process
// exits.
func BorrowWithinBudget(ctx context.Context, deps Deps, req Request, acq Acquisition, borrower string) (*Session, error) {
	opened, err := OpenWithinBudget(ctx, deps, req, acq)
	if err == nil {
		return opened, err
	}
	var held *lease.HeldError
	if !errors.As(err, &held) || !held.Known || held.Holder.Socket == "" {
		return nil, err
	}
	deps = deps.withDefaults()
	source := held.Holder
	req.Lender = deps.lenderFor(source)
	if acq.Progress != nil {
		acq.Progress = withoutLaunching(acq.Progress)
	}

	budget, stop := withClockTimeout(ctx, deps.Clock, YieldBudgetMs, errYieldBudget)
	defer stop()
	if !deps.requestYield(budget, source, borrower) {
		return nil, err
	}
	var last error
	for {
		opened, err := OpenWithinBudget(ctx, deps, req, acq)
		if err == nil {
			return opened, nil
		}
		var now *lease.HeldError
		if !errors.As(err, &now) {
			return nil, err
		}
		last = err
		if now.Known && now.Holder.PID != source.PID {
			return nil, err
		}
		if sleepErr := deps.Clock.Sleep(budget, borrowPollMs); sleepErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, last
		}
	}
}

// withoutLaunching forwards each phase to report except PhaseLaunching,
// which the first acquisition attempt already reported.
func withoutLaunching(report func(Phase)) func(Phase) {
	return func(p Phase) {
		if p != PhaseLaunching {
			report(p)
		}
	}
}

func (d Deps) lenderFor(source lease.Holder) device.RunnerOwner {
	if d.Owners == nil {
		return device.RunnerOwner{}
	}
	lender, err := d.Owners.ResolveOwner(source.PID)
	if err != nil {
		d.Logger.Warn("could not identify the holder as the lender of the device", logging.LogContext{"pid": source.PID, "error": err.Error()})
		return device.RunnerOwner{}
	}
	return lender
}

// requestYield sends the yield request and reports whether the
// acquisition should go ahead.
func (d Deps) requestYield(ctx context.Context, source lease.Holder, borrower string) bool {
	request := d.RequestYield
	if request == nil {
		request = attach.RequestYield
	}
	err := request(ctx, attach.ClientConfig{Path: source.Socket, Client: d.Command}, borrower)
	if err == nil {
		return true
	}
	switch code, _, _ := diag.Describe(err); code {
	case diag.CodeHolderBusy, diag.CodeUnsupported:
		return false
	}
	var ended *attach.ConnectionError
	var remote *attach.RemoteError
	return errors.As(err, &ended) && !errors.As(err, &remote) && !errors.Is(err, attach.ErrClientClosed)
}
