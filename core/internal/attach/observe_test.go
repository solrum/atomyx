package attach

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

type fakeSession struct{ status Status }

func (f fakeSession) Status() Status { return f.status }

type subscription struct {
	runID   string
	since   int64
	ctx     context.Context
	notices chan Notice
}

type fakeObserve struct {
	tree     TreeResult
	shot     ScreenshotResult
	fresh    FreshResult
	freshErr error
	// freshGate, when set, holds TreeFresh until it is closed or the
	// connection ends, and freshEntered reports that a call is held.
	freshGate    chan struct{}
	freshEntered chan struct{}
	subs         chan subscription
}

func newFakeObserve() *fakeObserve {
	return &fakeObserve{subs: make(chan subscription, 4), freshEntered: make(chan struct{}, 4)}
}

func (f *fakeObserve) TreeLatest() TreeResult             { return f.tree }
func (f *fakeObserve) ScreenshotLatest() ScreenshotResult { return f.shot }

func (f *fakeObserve) TreeFresh(ctx context.Context) (FreshResult, error) {
	if f.freshGate != nil {
		f.freshEntered <- struct{}{}
		select {
		case <-f.freshGate:
		case <-ctx.Done():
			return FreshResult{}, ctx.Err()
		}
	}
	return f.fresh, f.freshErr
}

func (f *fakeObserve) Subscribe(ctx context.Context, runID string, since int64) (<-chan Notice, error) {
	s := subscription{runID: runID, since: since, ctx: ctx, notices: make(chan Notice)}
	f.subs <- s
	return s.notices, nil
}

func (f *fakeObserve) nextSubscription(t *testing.T) subscription {
	t.Helper()
	select {
	case s := <-f.subs:
		return s
	case <-time.After(testGuard):
		t.Fatal("the handler was never asked to subscribe")
		return subscription{}
	}
}

func subscribed(t *testing.T, client *rawClient, h *fakeObserve, runID string, since int64) subscription {
	t.Helper()
	client.hello()
	if ok := client.call(TypeSubscribe, Subscribe{RunID: runID, SinceSeq: since}).Ok; ok == nil || !*ok {
		t.Fatal("subscribe was refused")
	}
	return h.nextSubscription(t)
}

func TestStatusComesFromTheSessionHandler(t *testing.T) {
	want := Status{State: "idle", Generation: 3, Holder: HolderInfo{PID: 9, Command: "atomyx run"}}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Session: fakeSession{want}}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	var got Status
	if err := client.call(TypeStatus, nil).Decode(&got); err != nil || got != want {
		t.Fatalf("status = %+v, %v, want %+v", got, err, want)
	}
}

func TestUnservedGroupsAreUnsupportedAndLeftOutOfTheFeatures(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	var hello HelloResult
	if err := client.hello().Decode(&hello); err != nil || len(hello.Features) != 0 {
		t.Fatalf("features = %v, %v, want none", hello.Features, err)
	}
	for _, typ := range []string{TypeStatus, TypeSubscribe, TypeTreeLatest, TypeScreenshotLatest, TypeTreeFresh} {
		if got := failureCode(t, client.call(typ, nil)); got != diag.CodeUnsupported {
			t.Fatalf("%s: code = %q, want %s", typ, got, diag.CodeUnsupported)
		}
	}
}

func TestObserveRequestsReturnTheHandlerData(t *testing.T) {
	h := newFakeObserve()
	h.tree = TreeResult{Revision: 4, Epoch: 2}
	h.shot = ScreenshotResult{Revision: 3, Epoch: 2, Format: "png", Image: []byte{1, 2, 3}}
	h.fresh = FreshResult{Paired: true, Scale: 2}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h}}, nil)
	client := dialRaw(t, srv.Path())

	var hello HelloResult
	if err := client.hello().Decode(&hello); err != nil || len(hello.Features) != 1 || hello.Features[0] != FeatureObserve {
		t.Fatalf("features = %v, %v, want [observe]", hello.Features, err)
	}
	var tree TreeResult
	if err := client.call(TypeTreeLatest, nil).Decode(&tree); err != nil || tree.Revision != 4 {
		t.Fatalf("tree = %+v, %v", tree, err)
	}
	var shot ScreenshotResult
	if err := client.call(TypeScreenshotLatest, nil).Decode(&shot); err != nil || string(shot.Image) != "\x01\x02\x03" {
		t.Fatalf("screenshot = %+v, %v", shot, err)
	}
	var fresh FreshResult
	if err := client.call(TypeTreeFresh, nil).Decode(&fresh); err != nil || !fresh.Paired || fresh.Scale != 2 {
		t.Fatalf("fresh = %+v, %v", fresh, err)
	}
}

func TestTreeFreshRefusalKeepsItsCode(t *testing.T) {
	h := newFakeObserve()
	h.freshErr = diag.NewHolderBusy("a tool call is running")
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	refusal := client.call(TypeTreeFresh, nil)
	if got := failureCode(t, refusal); got != diag.CodeHolderBusy || refusal.Remedy == "" {
		t.Fatalf("refusal = %+v, want HOLDER_BUSY with a remedy", refusal)
	}
}

func TestSubscribeAcknowledgesThenPushesNoticesInOrder(t *testing.T) {
	h := newFakeObserve()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h}}, nil)
	client := dialRaw(t, srv.Path())
	sub := subscribed(t, client, h, "run-7", 41)
	if sub.runID != "run-7" || sub.since != 41 {
		t.Fatalf("handler got run %q sinceSeq %d, want run-7 and 41", sub.runID, sub.since)
	}

	sub.notices <- Notice{Type: NoticeTree, Data: TreeNotice{Revision: 1, Epoch: 1}}
	sub.notices <- Notice{Type: NoticeScreenshot, Data: ScreenshotNotice{Revision: 2, Epoch: 1}}
	first, second := client.read(), client.read()
	if first.Kind() != KindNotice || first.Type != NoticeTree || second.Type != NoticeScreenshot {
		t.Fatalf("notices = %+v then %+v", first, second)
	}

	if got := failureCode(t, client.call(TypeSubscribe, Subscribe{})); got != diag.CodeInvalidArgument {
		t.Fatalf("second subscribe code = %q, want %s", got, diag.CodeInvalidArgument)
	}
}

func TestSlowReaderIsDisconnectedAndOthersKeepBeingServed(t *testing.T) {
	h := newFakeObserve()
	srv := startServer(t, ServerConfig{
		Handlers:     Handlers{Observe: h, Session: fakeSession{Status{State: "idle"}}},
		QueueSize:    1000,
		QueueBytes:   64 << 10,
		WriteTimeout: time.Hour,
	}, nil)
	slow := dialRaw(t, srv.Path())
	sub := subscribed(t, slow, h, "", 0)
	other := dialRaw(t, srv.Path())
	other.hello()

	detail := strings.Repeat("x", 16<<10)
	notice := Notice{Type: NoticeStep, Data: StepNotice{Line: StepLine{Detail: &detail}}}
	const floodLimit = 2000
	disconnected := false
	for i := 0; i < floodLimit && !disconnected; i++ {
		select {
		case sub.notices <- notice:
		case <-sub.ctx.Done():
			disconnected = true
		case <-time.After(testGuard):
			t.Fatal("the server stopped consuming notices instead of disconnecting the viewer")
		}
	}
	if !disconnected {
		t.Fatal("a viewer that never reads was not disconnected")
	}

	var status Status
	if err := other.call(TypeStatus, nil).Decode(&status); err != nil || status.State != "idle" {
		t.Fatalf("another viewer after the disconnect: %+v, %v", status, err)
	}
}

// writerGate holds the server's writer while armed, and reports each
// answered request, so a test can queue frames behind a frame that is
// not yet written.
type writerGate struct {
	armed      atomic.Bool
	entered    chan struct{}
	dispatched chan struct{}
	release    chan struct{}
	openOnce   sync.Once
}

// installGate wires a gate into cfg. The test must register g.open as a
// cleanup after starting the server, so the writer is released before
// the server waits for it.
func installGate(t *testing.T, cfg *ServerConfig) *writerGate {
	t.Helper()
	g := &writerGate{
		entered:    make(chan struct{}, 16),
		dispatched: make(chan struct{}, 16),
		release:    make(chan struct{}),
	}
	cfg.hooks.beforeWrite = func() {
		if g.armed.Load() {
			g.entered <- struct{}{}
			<-g.release
		}
	}
	cfg.hooks.afterDispatch = func() { g.dispatched <- struct{}{} }
	return g
}

func (g *writerGate) open() { g.openOnce.Do(func() { close(g.release) }) }

func (g *writerGate) waitFor(t *testing.T, signal chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(testGuard):
		t.Fatalf("%s never happened", what)
	}
}

func TestViewerOverTheByteBudgetIsDisconnected(t *testing.T) {
	h := newFakeObserve()
	cfg := ServerConfig{Handlers: Handlers{Observe: h}, QueueSize: 1000, QueueBytes: 1000}
	g := installGate(t, &cfg)
	srv := startServer(t, cfg, nil)
	t.Cleanup(g.open)
	sub := subscribed(t, dialRaw(t, srv.Path()), h, "", 0)
	g.armed.Store(true)

	detail := strings.Repeat("x", 300)
	notice := Notice{Type: NoticeStep, Data: StepNotice{Line: StepLine{Detail: &detail}}}
	const noticesPastBudget = 4
	for range noticesPastBudget {
		select {
		case sub.notices <- notice:
		case <-sub.ctx.Done():
		case <-time.After(testGuard):
			t.Fatal("the server stopped consuming notices")
		}
	}
	select {
	case <-sub.ctx.Done():
	case <-time.After(testGuard):
		t.Fatal("a viewer past the byte budget was not disconnected")
	}
}

func TestSecondPullResponseIsRefusedWhileTheFirstIsUnwritten(t *testing.T) {
	h := newFakeObserve()
	h.shot = ScreenshotResult{Format: "png", Image: []byte{1}}
	cfg := ServerConfig{Handlers: Handlers{Observe: h}}
	g := installGate(t, &cfg)
	srv := startServer(t, cfg, nil)
	t.Cleanup(g.open)
	client := dialRaw(t, srv.Path())
	client.hello()
	g.armed.Store(true)

	first := client.send(TypeScreenshotLatest, nil)
	g.waitFor(t, g.entered, "the writer holding the first response")
	second := client.send(TypeScreenshotLatest, nil)
	g.waitFor(t, g.dispatched, "the first request answered")
	g.waitFor(t, g.dispatched, "the second request answered")
	g.open()

	if got := client.read(); got.ID != first || got.Ok == nil || !*got.Ok {
		t.Fatalf("first response = %+v, want success for id %d", got, first)
	}
	refusal := client.read()
	if refusal.ID != second || failureCode(t, refusal) != diag.CodeHolderBusy {
		t.Fatalf("second response = %+v, want HOLDER_BUSY for id %d", refusal, second)
	}
	if ok := client.call(TypeScreenshotLatest, nil).Ok; ok == nil || !*ok {
		t.Fatal("a pull request after the first response was written was refused")
	}
}

func TestWriteDeadlineDisconnectsAViewerWhoseWriteFails(t *testing.T) {
	var stale atomic.Bool
	now := func() time.Time {
		if stale.Load() {
			return time.Unix(0, 0)
		}
		return time.Now()
	}
	h := newFakeObserve()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h}}, now)
	client := dialRaw(t, srv.Path())
	sub := subscribed(t, client, h, "", 0)

	stale.Store(true)
	select {
	case sub.notices <- Notice{Type: NoticeTree, Data: TreeNotice{}}:
	case <-time.After(testGuard):
		t.Fatal("notice was not consumed")
	}
	select {
	case <-sub.ctx.Done():
	case <-time.After(testGuard):
		t.Fatal("a write past its deadline did not end the connection")
	}
	client.expectClosed()
}

func TestOversizedResponseFailsAndTheConnectionStaysOpen(t *testing.T) {
	h := newFakeObserve()
	h.shot = ScreenshotResult{Image: make([]byte, 26<<20)}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h, Session: fakeSession{Status{State: "idle"}}}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()

	failure := client.call(TypeScreenshotLatest, nil)
	failureCode(t, failure)
	if !strings.Contains(failure.Message, "frame limit") {
		t.Fatalf("failure = %+v, want the frame limit named", failure)
	}
	if ok := client.call(TypeStatus, nil).Ok; ok == nil || !*ok {
		t.Fatal("the connection was unusable after the oversized response")
	}
}

func TestCloseCancelsSubscriptions(t *testing.T) {
	h := newFakeObserve()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Observe: h}}, nil)
	sub := subscribed(t, dialRaw(t, srv.Path()), h, "", 0)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sub.ctx.Done():
	default:
		t.Fatal("Close returned while a subscription context was still live")
	}
}
