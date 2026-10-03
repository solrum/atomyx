package inspector

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/session"
)

// fakeViewer is a target that follows another process: it has a view, a
// fresh capture and step lines, and no lease lifecycle.
type fakeViewer struct {
	mu        sync.Mutex
	info      session.Info
	snap      session.Snapshot
	freshErr  error
	lines     []runfolder.StepLine
	gap       bool
	feed      chan session.Observation
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeViewer() *fakeViewer {
	v := &fakeViewer{
		snap: sampleSnapshot(), feed: make(chan session.Observation, 8),
		done: make(chan struct{}), closed: make(chan struct{}),
	}
	v.info.State = session.StateIdle
	v.info.Holder.Command = "atomyx run"
	return v
}

func (v *fakeViewer) Info() session.Info       { return v.info }
func (v *fakeViewer) Latest() session.Snapshot { return v.snap }
func (v *fakeViewer) Subscribe(context.Context) (<-chan session.Observation, error) {
	return v.feed, nil
}

func (v *fakeViewer) SubscribeSince(context.Context, string, int64) (<-chan session.Observation, error) {
	return v.feed, nil
}

func (v *fakeViewer) StepsSince(runID string, since int64) ([]runfolder.StepLine, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []runfolder.StepLine
	for _, l := range v.lines {
		if runID != l.RunID || l.Seq > since {
			out = append(out, l)
		}
	}
	return out, v.gap, nil
}

func (v *fakeViewer) FreshTree(context.Context) (session.Snapshot, error) {
	return v.snap, v.freshErr
}

func (v *fakeViewer) Done() <-chan struct{} { return v.done }

func (v *fakeViewer) Close() error {
	v.closeOnce.Do(func() { close(v.closed) })
	return nil
}

var errNoReopen = errors.New("this test never reconnects")

type viewerHarness struct {
	*apiHarness
	host *Host
	clk  *clock.ManualClock
}

func newViewerHarness(t *testing.T, first *fakeViewer, open Opener) *viewerHarness {
	t.Helper()
	if open == nil {
		open = func(context.Context) (Target, error) { return nil, errNoReopen }
	}
	clk := clock.NewManualClock(1_000)
	host, err := NewHost(HostConfig{Target: first, Open: open, Clock: clk, IdleMs: testIdleMs, Logger: newRecordingLogger()})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Source: host, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{host.Routes(ReadRoutes)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Close()
		_ = host.Close()
	})
	return &viewerHarness{apiHarness: &apiHarness{srv: srv, token: authorize(t, srv)}, host: host, clk: clk}
}

func TestHostOverAViewer_RoutesServeItsSnapshots(t *testing.T) {
	v := newFakeViewer()
	h := newViewerHarness(t, v, nil)

	tree := h.get(routeTree)
	if tree.Code != http.StatusOK {
		t.Fatalf("tree = %d, body %s", tree.Code, tree.Body)
	}
	containsAll(t, tree.Body.String(), `"revision":9`, `"epoch":4`)
	shot := h.get(routeScreenshot)
	if shot.Code != http.StatusOK || shot.Body.String() != "\x89PNG-bytes" {
		t.Fatalf("screenshot = %d, body %q", shot.Code, shot.Body)
	}
	sess := h.get(routeSession)
	if sess.Code != http.StatusOK {
		t.Fatalf("session = %d, body %s", sess.Code, sess.Body)
	}
	containsAll(t, sess.Body.String(), `"command":"atomyx run"`, `"state":"idle"`)
}

func TestHostOverAViewer_FreshTreeMapsHolderBusyToTheBusyError(t *testing.T) {
	v := newFakeViewer()
	v.freshErr = diag.NewHolderBusy("the holder is running")
	h := newViewerHarness(t, v, nil)

	rec := h.post(routeTreeFresh, "")

	if rec.Code != http.StatusConflict || codeOf(t, rec) != diag.CodeHolderBusy {
		t.Fatalf("fresh = %d %s, want 409 %s", rec.Code, rec.Body, diag.CodeHolderBusy)
	}
}

func TestHostOverAViewer_NeverReleasesTheViewOnIdle(t *testing.T) {
	v := newFakeViewer()
	h := newViewerHarness(t, v, nil)

	h.clk.Advance(10 * testIdleMs)

	if rec := h.get(routeTree); rec.Code != http.StatusOK {
		t.Fatalf("tree after a long idle = %d, body %s, want the view kept", rec.Code, rec.Body)
	}
	select {
	case <-v.closed:
		t.Fatal("the view was closed by an idle release")
	default:
	}
}

func TestHostOverAViewer_AnExitedHolderShowsReleasedAndReconnectTakesTheNextView(t *testing.T) {
	first, next := newFakeViewer(), newFakeViewer()
	next.snap.TreeRevision = 21
	h := newViewerHarness(t, first, func(context.Context) (Target, error) { return next, nil })

	close(first.done)
	<-first.closed

	rec := h.get(routeTree)
	if rec.Code != http.StatusGone || codeOf(t, rec) != codeHolderExited {
		t.Fatalf("tree after the holder exited = %d %s, want 410 %s", rec.Code, rec.Body, codeHolderExited)
	}
	if got := h.host.Info().State; got != session.StateReleased {
		t.Fatalf("state = %q, want released", got)
	}
	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	containsAll(t, h.get(routeTree).Body.String(), `"revision":21`)
}

func TestDeferredRefresher_RefusesAsBusyUntilAHostIsBound(t *testing.T) {
	var refresher DeferredRefresher

	_, err := refresher.FreshTree(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeHolderBusy {
		t.Fatalf("FreshTree before Bind = %v, want %s", err, diag.CodeHolderBusy)
	}
}

func TestDeferredRefresher_ACaptureForAViewerKeepsTheIdleReleaseFromClosingTheSession(t *testing.T) {
	h := newHostHarness(t)
	var refresher DeferredRefresher
	refresher.Bind(h.host)
	h.first.duringFresh = func() {
		h.clk.Advance(testIdleMs)
		rearmed := make(chan struct{})
		go func() {
			h.clk.AwaitSleepers(1)
			close(rearmed)
		}()
		select {
		case <-rearmed:
		case <-h.first.closed:
			t.Error("the idle release closed the session during a viewer's capture")
		}
	}

	if _, err := refresher.FreshTree(context.Background()); err != nil {
		t.Fatal(err)
	}

	if h.released() {
		t.Fatal("the session was released by the time the capture ended")
	}
	if got := h.first.holderState(); got != "idle" {
		t.Fatalf("holder after the capture = %q, want idle", got)
	}
}
