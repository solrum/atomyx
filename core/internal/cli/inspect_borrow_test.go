package cli

import (
	"net/http"
	"syscall"
	"testing"

	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

// withYieldSeam makes the command serve sockets and send every yield
// request through seam.
func withYieldSeam(manager *lease.Manager, seam *sessiontest.YieldSeam) func(*Deps) {
	return func(d *Deps) {
		d.Session.Sockets = manager
		d.Session.RequestYield = seam.Requester()
	}
}

func TestInspect_ReconnectBorrowsTheDeviceFromAnIdleHolderThatCanYield(t *testing.T) {
	manager := newShortLeaseManager(t)
	seam := &sessiontest.YieldSeam{}
	run := startInspectWith(t, manager, heldDevice, nil, withYieldSeam(manager, seam))
	client := connectClient(t, run.stdout.awaitLine(t, "http://"))
	run.clk.AwaitSleepers(1)
	run.clk.Advance(inspectIdleMs)
	run.stderr.awaitLine(t, "inspector.idle.released")
	holder := holdYieldableDevice(t, manager)

	if status := client.send(t, http.MethodPost, "/api/v1/reconnect", "", nil); status != http.StatusOK {
		t.Fatalf("reconnect = %d, want 200", status)
	}

	if got := holder.borrower.Load(); got != inspectCommandName {
		t.Fatalf("the holder was told the borrower is %v, want %q", got, inspectCommandName)
	}
	if info := client.session(t); info.State != string(session.StateIdle) || info.Holder.Command != inspectCommandName {
		t.Fatalf("session after reconnect = %+v, want the inspector to hold the device idle", info)
	}
	run.stop(syscall.SIGINT)
}

func TestInspect_NothingButAReconnectEverAsksAHolderToYield(t *testing.T) {
	manager := newShortLeaseManager(t)
	seam := &sessiontest.YieldSeam{}
	holder := holdYieldableDevice(t, manager)
	run := startInspectWith(t, manager, heldDevice, nil, withYieldSeam(manager, seam))
	client := connectClient(t, awaitInspectURL(t, run))
	if got := seam.Calls(); got != 0 || holder.borrower.Load() != nil {
		t.Fatalf("the inspector asked the holder to yield while opening (%d requests), want it to watch instead", got)
	}

	_ = holder.sess.Close()
	run.stderr.awaitLine(t, "inspector.target.ended")

	if got := seam.Calls(); got != 0 {
		t.Fatalf("the inspector sent %d yield requests on its own, want none", got)
	}
	if info := client.session(t); info.State != string(session.StateReleased) {
		t.Fatalf("state = %q, want released with no automatic take-over", info.State)
	}
	run.stop(syscall.SIGINT)
}

func TestInspect_AnIdleReleaseNeverAsksAHolderToYield(t *testing.T) {
	manager := newShortLeaseManager(t)
	seam := &sessiontest.YieldSeam{}
	run := startInspectWith(t, manager, heldDevice, nil, withYieldSeam(manager, seam))
	client := connectClient(t, run.stdout.awaitLine(t, "http://"))
	run.clk.AwaitSleepers(1)
	run.clk.Advance(inspectIdleMs)
	run.stderr.awaitLine(t, "inspector.idle.released")
	holdYieldableDevice(t, manager)

	run.clk.Advance(inspectIdleMs)

	if got := seam.Calls(); got != 0 {
		t.Fatalf("the inspector sent %d yield requests after its idle release, want none", got)
	}
	if state := client.state(t); state != "released" {
		t.Fatalf("state = %q, want released until the operator reconnects", state)
	}
	run.stop(syscall.SIGINT)
}
