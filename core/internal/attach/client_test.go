package attach

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

func dialClient(t *testing.T, srv *Server, cfg ClientConfig) *Client {
	t.Helper()
	cfg.Path = srv.Path()
	c, err := Dial(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func observeServer(t *testing.T, h *fakeObserve, cfg ServerConfig) *Server {
	t.Helper()
	cfg.Handlers = Handlers{Observe: h, Session: fakeSession{Status{State: "idle"}}}
	return startServer(t, cfg, nil)
}

func TestDialCompletesHelloAndCallsReturnData(t *testing.T) {
	srv := observeServer(t, newFakeObserve(), ServerConfig{})
	c := dialClient(t, srv, ClientConfig{Client: "test viewer"})
	if c.Protocol() != 1 || len(c.Features()) != 1 || c.Features()[0] != FeatureObserve {
		t.Fatalf("protocol %d features %v", c.Protocol(), c.Features())
	}
	var status Status
	if err := c.Call(context.Background(), TypeStatus, nil, &status); err != nil || status.State != "idle" {
		t.Fatalf("status = %+v, %v", status, err)
	}
}

func TestDialRefusesAHolderRunningAsAnotherUser(t *testing.T) {
	srv := observeServer(t, newFakeObserve(), ServerConfig{})
	other := uint32(os.Getuid()) + 1
	_, err := Dial(context.Background(), ClientConfig{
		Path:    srv.Path(),
		PeerUID: func(*net.UnixConn) (uint32, error) { return other, nil },
	})
	var uidErr *PeerUIDError
	if !errors.As(err, &uidErr) || uidErr.Got != other {
		t.Fatalf("error = %v, want a PeerUIDError for uid %d", err, other)
	}
}

func TestRemoteFailureCarriesItsCodeAndRemedy(t *testing.T) {
	h := newFakeObserve()
	h.freshErr = diag.NewHolderBusy("a tool call is running")
	c := dialClient(t, observeServer(t, h, ServerConfig{}), ClientConfig{})
	err := c.Call(context.Background(), TypeTreeFresh, nil, nil)
	var remote *RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("error = %v, want a RemoteError", err)
	}
	code, _, remedy := diag.Describe(err)
	if code != diag.CodeHolderBusy || remedy == "" || remote.Type != TypeTreeFresh {
		t.Fatalf("code %q remedy %q type %q", code, remedy, remote.Type)
	}
}

func TestDialOverTheCapReportsTheRefusal(t *testing.T) {
	srv := observeServer(t, newFakeObserve(), ServerConfig{MaxConnections: 1})
	dialClient(t, srv, ClientConfig{})
	_, err := Dial(context.Background(), ClientConfig{Path: srv.Path()})
	var ended *ConnectionError
	if !errors.As(err, &ended) {
		t.Fatalf("error = %v, want a ConnectionError", err)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeHolderBusy {
		t.Fatalf("code = %q, want %s", code, diag.CodeHolderBusy)
	}
}

func TestNoticesArriveInOrderAndEndWithTheConnection(t *testing.T) {
	h := newFakeObserve()
	srv := observeServer(t, h, ServerConfig{})
	c := dialClient(t, srv, ClientConfig{})
	if err := c.Call(context.Background(), TypeSubscribe, Subscribe{}, nil); err != nil {
		t.Fatal(err)
	}
	sub := h.nextSubscription(t)
	sub.notices <- Notice{Type: NoticeTree, Data: TreeNotice{Revision: 1}}
	sub.notices <- Notice{Type: NoticeScreenshot, Data: ScreenshotNotice{Revision: 2}}
	for _, want := range []string{NoticeTree, NoticeScreenshot} {
		select {
		case n := <-c.Notices():
			if n.Type != want || n.Kind() != KindNotice {
				t.Fatalf("notice = %+v, want %s", n, want)
			}
		case <-time.After(testGuard):
			t.Fatalf("notice %s never arrived", want)
		}
	}

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(testGuard):
		t.Fatal("Done never closed after the holder exited")
	}
	for range c.Notices() {
	}
	var ended *ConnectionError
	if !errors.As(c.Err(), &ended) {
		t.Fatalf("Err = %v, want a ConnectionError", c.Err())
	}
}

func TestPendingCallReturnsTheConnectionErrorWhenTheHolderExits(t *testing.T) {
	h := newFakeObserve()
	h.freshGate = make(chan struct{})
	srv := observeServer(t, h, ServerConfig{})
	c := dialClient(t, srv, ClientConfig{})

	result := make(chan error, 1)
	go func() { result <- c.Call(context.Background(), TypeTreeFresh, nil, nil) }()
	<-h.freshEntered
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	var ended *ConnectionError
	select {
	case err := <-result:
		if !errors.As(err, &ended) {
			t.Fatalf("pending call error = %v, want a ConnectionError", err)
		}
	case <-time.After(testGuard):
		t.Fatal("a pending call outlived the connection")
	}
	if err := c.Call(context.Background(), TypeStatus, nil, nil); !errors.As(err, &ended) {
		t.Fatalf("call after the end = %v, want a ConnectionError", err)
	}
}

func TestCancelledCallLeavesTheConnectionUsable(t *testing.T) {
	h := newFakeObserve()
	h.freshGate = make(chan struct{})
	c := dialClient(t, observeServer(t, h, ServerConfig{}), ClientConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- c.Call(ctx, TypeTreeFresh, nil, nil) }()
	<-h.freshEntered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	close(h.freshGate)
	if err := c.Call(context.Background(), TypeStatus, nil, nil); err != nil {
		t.Fatalf("call after a cancelled call: %v", err)
	}
}

func TestCloseIsTypedAndReleasesAConsumerThatStoppedReading(t *testing.T) {
	baseGoroutines := runtime.NumGoroutine()
	baseFDs := openFDs(t)

	h := newFakeObserve()
	srv := observeServer(t, h, ServerConfig{})
	c := dialClient(t, srv, ClientConfig{NoticeBuffer: 1})
	if err := c.Call(context.Background(), TypeSubscribe, Subscribe{}, nil); err != nil {
		t.Fatal(err)
	}
	sub := h.nextSubscription(t)
	for range 3 {
		sub.notices <- Notice{Type: NoticeTree, Data: TreeNotice{}}
	}
	if err := c.Call(context.Background(), TypeStatus, nil, nil); err != nil {
		t.Fatalf("a call while notices wait for the consumer: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		_ = c.Close()
		_ = c.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(testGuard):
		t.Fatal("Close blocked behind a consumer that stopped reading")
	}
	if !errors.Is(c.Err(), ErrClientClosed) {
		t.Fatalf("Err = %v, want ErrClientClosed", c.Err())
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	waitForNoLeak(t, baseGoroutines, baseFDs)
}

func TestConsumerThatCallsFromItsNoticeLoopNeverHangs(t *testing.T) {
	h := newFakeObserve()
	h.tree = TreeResult{Revision: 9}
	c := dialClient(t, observeServer(t, h, ServerConfig{}), ClientConfig{NoticeBuffer: 1})
	if err := c.Call(context.Background(), TypeSubscribe, Subscribe{}, nil); err != nil {
		t.Fatal(err)
	}
	sub := h.nextSubscription(t)

	const burst = 200
	go func() {
		for i := range burst {
			sub.notices <- Notice{Type: NoticeTree, Data: TreeNotice{Revision: uint64(i)}}
		}
		sub.notices <- Notice{Type: NoticeStep, Data: StepNotice{}}
	}()

	failed := make(chan error, 1)
	go func() {
		for n := range c.Notices() {
			if n.Type == NoticeStep {
				failed <- nil
				return
			}
			var tree TreeResult
			if err := c.Call(context.Background(), TypeTreeLatest, nil, &tree); err != nil {
				failed <- err
				return
			}
		}
		failed <- errors.New("the notices ended before the last step notice")
	}()
	select {
	case err := <-failed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testGuard):
		t.Fatal("a consumer calling from its notice loop hung during a notice burst")
	}
}

func TestInboxKeepsOnlyTheNewestTreeAndScreenshotNotice(t *testing.T) {
	in := newNoticeInbox(4)
	tree := func(rev uint64) Frame {
		f, err := NewNotice(NoticeTree, TreeNotice{Revision: rev})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	shot, err := NewNotice(NoticeScreenshot, ScreenshotNotice{})
	if err != nil {
		t.Fatal(err)
	}
	in.push(tree(1))
	in.push(shot)
	in.push(tree(2))
	in.push(tree(3))

	first, _, _ := in.next()
	second, _, _ := in.next()
	if first.Type != NoticeScreenshot || second.Type != NoticeTree {
		t.Fatalf("order = %s then %s, want screenshot then tree", first.Type, second.Type)
	}
	var newest TreeNotice
	if err := second.Decode(&newest); err != nil || newest.Revision != 3 {
		t.Fatalf("kept tree notice = %+v, %v, want revision 3", newest, err)
	}
	if _, ok, _ := in.next(); ok {
		t.Fatal("an older tree notice was kept")
	}
}

func TestInboxReplacesStepsPastTheBoundWithOneGapMarker(t *testing.T) {
	in := newNoticeInbox(2)
	step := func(seq int64) Frame {
		f, err := NewNotice(NoticeStep, StepNotice{Line: StepLine{Seq: seq}})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	for seq := int64(1); seq <= 5; seq++ {
		in.push(step(seq))
	}
	var got []string
	for {
		f, ok, _ := in.next()
		if !ok {
			break
		}
		got = append(got, f.Type)
	}
	want := []string{NoticeStep, NoticeStep, NoticeStepsDropped}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("delivered %v, want %v", got, want)
	}

	in.push(step(6))
	in.push(step(7))
	in.push(step(8))
	got = got[:0]
	for {
		f, ok, _ := in.next()
		if !ok {
			break
		}
		got = append(got, f.Type)
	}
	want = []string{NoticeStep, NoticeStep, NoticeStepsDropped}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("after the marker was delivered, delivered %v, want %v", got, want)
	}
}

func TestDuplicateResponseIsDroppedWithoutBlockingTheReadLoop(t *testing.T) {
	c := &Client{pending: map[int64]chan Frame{7: make(chan Frame, 1)}}
	ok := true
	response := Frame{V: ProtocolMajor, ID: 7, Type: TypeStatus, Ok: &ok}
	returned := make(chan struct{})
	go func() {
		c.reply(response)
		c.reply(response)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(testGuard):
		t.Fatal("a duplicate response blocked the reader")
	}
}

func TestCancellingTheContextEndsABlockedWrite(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	c := newClient(local, 0)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- c.Call(ctx, TypeStatus, nil, nil) }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(testGuard):
		t.Fatal("cancelling the context did not end a write nobody reads")
	}
}

func TestDialReportsTheRefusalWhenTheHolderClosedBeforeTheHelloWasSent(t *testing.T) {
	path := shortSocketPath(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	holderClosed := make(chan struct{})
	go func() {
		defer close(holderClosed)
		nc, err := ln.AcceptUnix()
		if err != nil {
			return
		}
		_ = WriteFrame(nc, NewErrorResponse(0, TypeHello, diag.NewHolderBusy("full")))
		_ = nc.Close()
	}()

	_, err = Dial(context.Background(), ClientConfig{
		Path: path,
		PeerUID: func(*net.UnixConn) (uint32, error) {
			<-holderClosed
			return uint32(os.Getuid()), nil
		},
	})
	if code, _, _ := diag.Describe(err); code != diag.CodeHolderBusy {
		t.Fatalf("error = %v (code %q), want the HOLDER_BUSY refusal", err, code)
	}
}
