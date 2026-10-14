package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
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
// processes read when they find the device held, and routes the holder's
// interventions into the run folder until the run ends. It is
// best-effort, because the metadata is display-only.
func (d *deviceSession) recordRun(record *runRecord) {
	d.SetInterventionRecorder(record.writer)
	record.onEnd = func() { d.SetInterventionRecorder(nil) }
	info := d.Built().Driver.AgentInfo()
	_ = record.writer.Update(func(m *runfolder.Meta) {
		runfolder.RecordAgent(m, info.ProtocolVersion, info.AgentVersion)
	})
	_ = d.SetRunning(record.ID())
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
	opened, err := acquireSession(ctx, deps, command, desc, nil, nil)
	if err != nil {
		return nil, err
	}
	return &deviceSession{Session: opened}, nil
}

// borrowDevice is openDevice for a command that may take a device from an
// idle holder able to yield it. borrower is the command string the holder
// shows to its own user, for example "atomyx run login.yml".
func borrowDevice(ctx context.Context, deps Deps, command, borrower string, desc device.Descriptor) (*deviceSession, error) {
	return borrowDebuggable(ctx, deps, command, borrower, desc, nil)
}

// borrowDebuggable is borrowDevice for a command that can pause its run.
// A nil debugger leaves the session undebuggable.
func borrowDebuggable(ctx context.Context, deps Deps, command, borrower string, desc device.Descriptor, debugger session.Debugger) (*deviceSession, error) {
	opened, err := borrowSession(ctx, deps, command, borrower, session.Request{Descriptor: desc, Debugger: debugger})
	if err != nil {
		return nil, err
	}
	return &deviceSession{Session: opened}, nil
}

// borrowSession is borrowDevice for a command that manages its release
// itself and shapes the request, for example to serve viewers.
func borrowSession(ctx context.Context, deps Deps, command, borrower string, req session.Request) (*session.Session, error) {
	return acquireWith(ctx, deps, command, req, func(sessionDeps session.Deps, req session.Request, acq session.Acquisition) (*session.Session, error) {
		return session.Borrow(ctx, sessionDeps, req, acq, borrower)
	})
}

// acquireSession is the acquisition openDevice performs, returning the
// bare session for a command that manages its release itself.
// socketRefresher, when set, answers a viewer's fresh capture request on the
// holder socket in place of the session's own capture.
// yielder, when set, lets another process ask this command to give the device
// up, and makes the session advertise that it can.
func acquireSession(ctx context.Context, deps Deps, command string, desc device.Descriptor, socketRefresher session.Refresher, yielder session.Yielder) (*session.Session, error) {
	return acquireWith(ctx, deps, command, session.Request{Descriptor: desc, SocketRefresher: socketRefresher, Yielder: yielder}, func(d session.Deps, req session.Request, acq session.Acquisition) (*session.Session, error) {
		return session.Acquire(ctx, d, req, acq)
	})
}

// acquireWith builds the session dependencies and acquisition budgets for
// command and hands them to acquire.
func acquireWith(ctx context.Context, deps Deps, command string, req session.Request, acquire func(session.Deps, session.Request, session.Acquisition) (*session.Session, error)) (*session.Session, error) {
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
	return acquire(sessionDeps, req, session.Acquisition{BudgetMs: deps.Timeouts.DeviceAcquireMs, BuildBudgetMs: deps.Timeouts.RunnerBuildMs})
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
