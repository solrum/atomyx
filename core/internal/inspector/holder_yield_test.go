package inspector

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

const yieldBorrower = "atomyx run login.yml"

func codeOfErr(err error) string {
	code, _, _ := diag.Describe(err)
	return code
}

func followingViewer(v *fakeViewer) Opener {
	return func(context.Context) (Target, error) { return v, nil }
}

func TestHostYield_ACaptureInFlightRefusesWithHolderBusy(t *testing.T) {
	h := newHostHarnessFollowing(t, nil)
	var during error
	h.first.duringFresh = func() { _, during = h.host.Yield(context.Background(), yieldBorrower) }

	if rec := h.post(routeTreeFresh, ""); rec.Code != http.StatusOK {
		t.Fatalf("fresh = %d, body %s", rec.Code, rec.Body)
	}

	if got := codeOfErr(during); got != diag.CodeHolderBusy {
		t.Fatalf("yield during a capture = %v, want %s", during, diag.CodeHolderBusy)
	}
	if h.first.closes.Load() != 0 {
		t.Fatal("a refused yield closed the session")
	}
}

func TestHostYield_ARefreshOfAViewerCaptureRefusesWithHolderBusy(t *testing.T) {
	h := newHostHarnessFollowing(t, nil)
	var refresher DeferredRefresher
	refresher.Bind(h.host)
	var yielder DeferredYielder
	yielder.Bind(h.host)
	var during error
	h.first.duringFresh = func() { _, during = yielder.Yield(context.Background(), yieldBorrower) }

	if _, err := refresher.FreshTree(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := codeOfErr(during); got != diag.CodeHolderBusy {
		t.Fatalf("yield during a socket refresh = %v, want %s", during, diag.CodeHolderBusy)
	}
}

func TestDeferredYielder_RefusesAsBusyUntilAHostIsBound(t *testing.T) {
	var yielder DeferredYielder

	_, err := yielder.Yield(context.Background(), yieldBorrower)

	if got := codeOfErr(err); got != diag.CodeHolderBusy {
		t.Fatalf("Yield before Bind = %v, want %s", err, diag.CodeHolderBusy)
	}
}

func TestHostYield_AViewOfAnotherProcessCannotYield(t *testing.T) {
	h := newViewerHarness(t, newFakeViewer(), nil)

	_, err := h.host.Yield(context.Background(), yieldBorrower)

	if got := codeOfErr(err); got != diag.CodeHolderBusy {
		t.Fatalf("yield of a watching host = %v, want %s", err, diag.CodeHolderBusy)
	}
}

func TestHostYield_AnIdleHostClosesRecordsTheBorrowerAndWatchesIt(t *testing.T) {
	borrower := newFakeViewer()
	h := newHostHarnessFollowing(t, followingViewer(borrower))

	afterReply, err := h.host.Yield(context.Background(), yieldBorrower)
	if err != nil {
		t.Fatalf("yield of an idle host: %v", err)
	}
	if got := h.host.Info().State; got != session.StateBorrowed {
		t.Fatalf("state between the yield and the close = %q, want borrowed", got)
	}
	afterReply()

	if got := <-h.first.borrowers; got != session.InterventionControlBorrowed+":"+yieldBorrower {
		t.Fatalf("recorded intervention = %q", got)
	}
	if h.first.closes.Load() != 1 {
		t.Fatalf("closes = %d, want one", h.first.closes.Load())
	}
	if rec := h.get(routeTree); rec.Code != http.StatusOK {
		t.Fatalf("tree while watching the borrower = %d, body %s", rec.Code, rec.Body)
	}

	close(borrower.done)
	<-borrower.closed

	rec := h.get(routeTree)
	if rec.Code != http.StatusGone || codeOf(t, rec) != codeHolderExited {
		t.Fatalf("tree after the borrower ended = %d %s, want 410 %s", rec.Code, rec.Body, codeHolderExited)
	}
	if got := h.host.Info().State; got != session.StateReleased {
		t.Fatalf("state after the borrower ended = %q, want released", got)
	}
}

func TestHostYield_ANobodyToFollowLeavesTheHostReleased(t *testing.T) {
	h := newHostHarnessFollowing(t, func(context.Context) (Target, error) { return nil, session.ErrNoBorrower })

	afterReply, err := h.host.Yield(context.Background(), yieldBorrower)
	if err != nil {
		t.Fatal(err)
	}
	afterReply()

	if got := h.host.Info().State; got != session.StateReleased {
		t.Fatalf("state = %q, want released when no process took the device", got)
	}
	if rec := h.get(routeTree); rec.Code != http.StatusGone || codeOf(t, rec) != codeDeviceReleased {
		t.Fatalf("tree = %d %s, want 410 %s", rec.Code, rec.Body, codeDeviceReleased)
	}
}

func TestHostYield_AReconnectDuringTheFollowWins(t *testing.T) {
	release := make(chan struct{})
	slow := newFakeViewer()
	h := newHostHarnessFollowing(t, func(context.Context) (Target, error) {
		<-release
		return slow, nil
	})
	afterReply, err := h.host.Yield(context.Background(), yieldBorrower)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { afterReply(); close(done) }()
	<-h.first.closed

	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	close(release)
	<-done

	<-slow.closed
}

func TestHostYield_ARaceWithTheIdleReleaseClosesTheSessionOnce(t *testing.T) {
	h := newHostHarnessFollowing(t, nil)
	var wg sync.WaitGroup
	var afterReply func()
	wg.Add(2)
	go func() {
		defer wg.Done()
		h.clk.Advance(testIdleMs)
	}()
	go func() {
		defer wg.Done()
		afterReply, _ = h.host.Yield(context.Background(), yieldBorrower)
	}()
	wg.Wait()
	if afterReply != nil {
		afterReply()
	}
	h.host.opMu.Lock()
	h.host.opMu.Unlock() //nolint:staticcheck // the empty section is the barrier

	if got := h.first.closes.Load(); got != 1 {
		t.Fatalf("closes = %d, want exactly one", got)
	}
}
