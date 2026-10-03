package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

// deviceSession is a device a CLI command owns for the length of one
// command.
type deviceSession struct {
	*session.Session
}

// Close releases the device. It is best-effort: the command already has
// its result and exit code, so a failed teardown has nothing left to
// report to.
func (d *deviceSession) Close() {
	_ = d.Session.Close()
}

// recordRun records the open run's id in the holder metadata other
// processes read when they find the device held. It is best-effort,
// because the metadata is display-only.
func (d *deviceSession) recordRun(runID string) {
	_ = d.SetRunning(runID)
}

// cancelOnLost cancels the command's context when the session loses the
// device, so a run stops instead of driving a device another process
// may now own. The watcher ends when the session closes.
func (d *deviceSession) cancelOnLost(cancel context.CancelCauseFunc) {
	go func() {
		select {
		case <-d.Lost():
			cancel(d.LostErr())
		case <-d.Closed():
		}
	}()
}

// openDevice acquires the device named by desc through session.Acquire on
// behalf of command: the runner build runs before any lease is taken, then
// the acquisition runs within deps.Timeouts.DeviceAcquireMs. The command is the name other
// processes see when they find the device held, for example "atomyx run".
func openDevice(ctx context.Context, deps Deps, command string, desc device.Descriptor) (*deviceSession, error) {
	opened, err := acquireSession(ctx, deps, command, desc)
	if err != nil {
		return nil, err
	}
	if sessionOpened != nil {
		sessionOpened(opened)
	}
	return &deviceSession{Session: opened}, nil
}

// sessionOpened lets a test observe the session a command opened. It is
// nil in production.
var sessionOpened func(*session.Session)

// acquireSession is the acquisition openDevice performs, returning the
// bare session for a command that manages its release itself.
func acquireSession(ctx context.Context, deps Deps, command string, desc device.Descriptor) (*session.Session, error) {
	sessionDeps := deps.Session
	sessionDeps.Factory = deps.Factory
	if sessionDeps.Command == "" {
		sessionDeps.Command = command
	}
	if sessionDeps.Role == "" {
		sessionDeps.Role = "cli"
	}
	if sessionDeps.Logger == nil {
		sessionDeps.Logger = deps.logger()
	}
	return session.Acquire(ctx, sessionDeps, session.Request{Descriptor: desc}, session.Acquisition{BudgetMs: deps.Timeouts.DeviceAcquireMs, BuildBudgetMs: deps.Timeouts.RunnerBuildMs})
}

// reportLost tells the operator that a run stopped because the device
// was lost, when it was, and reports whether it did.
func (d *deviceSession) reportLost(stderr io.Writer) bool {
	err := d.LostErr()
	if err == nil {
		return false
	}
	_, _ = fmt.Fprintf(stderr, "error: the device was lost during the command, so it was stopped: %s\n", err)
	return true
}

// heldHolder extracts the holder of a device another process drives
// from an Open error, and reports whether err is that case.
func heldHolder(err error) (lease.Holder, bool, bool) {
	var held *lease.HeldError
	if !errors.As(err, &held) {
		return lease.Holder{}, false, false
	}
	return held.Holder, held.Known, true
}
