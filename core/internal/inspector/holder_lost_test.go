package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

func TestHost_ALostDeviceIsClosedDroppedAndReportedAsLost(t *testing.T) {
	h := newHostHarness(t)
	close(h.first.lost)
	<-h.first.closed

	rec := h.get(routeSession)
	var info struct{ State string }
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || rec.Code != http.StatusOK || info.State != string(session.StateLost) {
		t.Fatalf("session = %d %s, want 200 with state lost", rec.Code, rec.Body)
	}
	for _, path := range []string{routeTree, routeScreenshot, routeEvents} {
		rec := h.get(path)
		if rec.Code != http.StatusGone || codeOf(t, rec) != codeDeviceLost {
			t.Errorf("GET %s = %d %s, want 410 %s", path, rec.Code, rec.Body, codeDeviceLost)
		}
	}
	if rec := h.post(routeTreeFresh, ""); rec.Code != http.StatusGone || codeOf(t, rec) != codeDeviceLost {
		t.Errorf("fresh = %d %s, want 410 %s", rec.Code, rec.Body, codeDeviceLost)
	}
	if _, err := h.host.FreshTree(context.Background()); !errors.Is(err, ErrLost) {
		t.Fatalf("FreshTree after the loss = %v, want ErrLost", err)
	}
}

func TestHost_ReconnectRecoversALostDeviceThroughTheOpener(t *testing.T) {
	h := newHostHarness(t)
	close(h.first.lost)
	<-h.first.closed

	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	if h.dev.opens != 2 {
		t.Fatalf("opens = %d, want 2", h.dev.opens)
	}
	if rec := h.get(routeTree); rec.Code != http.StatusOK {
		t.Fatalf("tree after the reconnect = %d, body %s", rec.Code, rec.Body)
	}
	if got := h.host.Info().State; got == session.StateLost {
		t.Fatalf("state after the reconnect = %q, want the new device's state", got)
	}
}

func TestHost_ReconnectAfterALossReportsAHeldDeviceAndStaysLost(t *testing.T) {
	h := newHostHarness(t)
	close(h.first.lost)
	<-h.first.closed
	other, err := h.dev.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec := h.post(routeReconnect, "")
	if rec.Code != http.StatusConflict || codeOf(t, rec) != diag.CodeDeviceHeld {
		t.Fatalf("reconnect = %d %s, want 409 %s", rec.Code, rec.Body, diag.CodeDeviceHeld)
	}
	if got := h.host.Info().State; got != session.StateLost {
		t.Fatalf("state = %q, want lost until a reconnect succeeds", got)
	}
	_ = other.Close()
}

func TestHost_ALossAfterAReleaseIsIgnored(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	close(h.first.lost)
	if got := h.host.Info().State; got != session.StateReleased {
		t.Fatalf("state = %q, want released: the device was given up before it was lost", got)
	}
}

func TestHost_IdleNeverReleasesWhileAnActionIsDriving(t *testing.T) {
	h := newHostHarness(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	h.first.duringFresh = func() {
		close(entered)
		<-resume
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.host.FreshTree(context.Background())
		done <- err
	}()
	<-entered

	h.clk.Advance(testIdleMs)
	slept := make(chan struct{})
	go func() {
		h.clk.AwaitSleepers(1)
		close(slept)
	}()
	select {
	case <-slept:
	case <-h.first.closed:
		t.Fatal("the device was released while a capture was driving it")
	}

	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(testIdleMs)
	<-h.first.closed
}

func TestHost_ADrivingActionThatEndsAfterAReleaseLeavesTheNewDeviceIdleable(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	if err := h.host.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := h.host.held()

	end, err := h.host.beginDriving(h.first)
	if !errors.Is(err, ErrReleased) {
		t.Fatalf("beginDriving on a released device = %v, want ErrReleased", err)
	}
	if end != nil {
		end()
	}
	if got := h.host.drivingCount(); got != 0 {
		t.Fatalf("driving = %d after a refused action, want 0", got)
	}
	h.clk.AwaitSleepers(1)
	h.clk.Advance(testIdleMs)
	<-second.(*fakeHeld).closed
}

func TestHost_ADrivingActionEndingAfterALossDoesNotTouchTheNextDevice(t *testing.T) {
	h := newHostHarness(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	h.first.duringFresh = func() {
		close(entered)
		<-resume
	}
	done := make(chan error, 1)
	go func() {
		_, err := h.host.FreshTree(context.Background())
		done <- err
	}()
	<-entered
	close(h.first.lost)
	<-h.first.closed
	if err := h.host.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := h.host.held()

	close(resume)
	<-done
	if got := h.host.drivingCount(); got != 0 {
		t.Fatalf("driving = %d after a stale action ended, want 0", got)
	}
	if got := second.(*fakeHeld).holderState(); got != "idle" {
		t.Fatalf("the new device's holder = %q, want idle: a stale action must not mark it", got)
	}
	h.clk.AwaitSleepers(1)
	h.clk.Advance(testIdleMs)
	<-second.(*fakeHeld).closed
}

func (h *Host) drivingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.driving
}
