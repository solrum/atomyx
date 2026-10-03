package inspector

import (
	"context"
	"errors"
	"sync/atomic"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// Source is what the route groups read and ask of a device: the view of
// what it captured, and a fresh capture on request. It has no activity
// gate, so a view of another process's device satisfies it as well as a
// device this process holds.
type Source interface {
	session.Viewer
	session.Refresher
}

// DeferredRefresher answers a fresh capture through a host that is bound
// after the refresher is created. A process that opens its device before
// it builds the host hands this to the session as the refresher of its
// attach socket, so a viewer's capture request is counted as driving the
// device and an idle release cannot close the session during it. Until a
// host is bound, or while the host holds no device, a request is refused
// as busy.
type DeferredRefresher struct {
	host atomic.Pointer[Host]
}

var _ session.Refresher = (*DeferredRefresher)(nil)

// Bind makes host answer every later request.
func (d *DeferredRefresher) Bind(host *Host) { d.host.Store(host) }

// FreshTree captures through the bound host.
func (d *DeferredRefresher) FreshTree(ctx context.Context) (session.Snapshot, error) {
	host := d.host.Load()
	if host == nil {
		return session.Snapshot{}, diag.NewHolderBusy("the inspector is starting")
	}
	snap, err := host.FreshTree(ctx)
	if errors.Is(err, ErrReleased) || errors.Is(err, ErrLost) || errors.Is(err, ErrHolderExited) {
		return session.Snapshot{}, diag.NewHolderBusy("the inspector holds no device at the moment")
	}
	return snap, err
}
