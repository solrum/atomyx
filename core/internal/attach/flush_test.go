package attach

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerCloseDeliversFramesAlreadyQueued(t *testing.T) {
	const queued = 6
	var writes atomic.Int32
	release := make(chan struct{})
	dispatched := make(chan struct{}, queued+1)
	cfg := ServerConfig{Handlers: Handlers{Session: fakeSession{Status{State: "idle"}}}}
	cfg.hooks.beforeWrite = func() {
		if writes.Add(1) == 2 {
			<-release
		}
	}
	cfg.hooks.afterDispatch = func() { dispatched <- struct{}{} }
	srv := startServer(t, cfg, nil)
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseWriter)

	client := dialRaw(t, srv.Path())
	client.hello()
	for range queued {
		client.send(TypeStatus, nil)
		select {
		case <-dispatched:
		case <-time.After(testGuard):
			t.Fatal("a request was never dispatched")
		}
	}

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = srv.Close()
	}()
	waitUntil(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.closed
	})
	time.Sleep(100 * time.Millisecond)
	releaseWriter()

	for i := range queued {
		if reply := client.read(); reply.Ok == nil || !*reply.Ok {
			t.Fatalf("reply %d = %+v, want ok", i, reply)
		}
	}
	client.expectClosed()
	select {
	case <-closed:
	case <-time.After(testGuard):
		t.Fatal("Close never returned")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testGuard)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestReplyIsDeliveredBeforeTheHolderClosesItsServer(t *testing.T) {
	var srv *Server
	control := &fakeControl{}
	control.after = func() { _ = srv.Close() }
	srv = startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	c, err := Dial(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower", Features: []string{FeatureControl}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Call(context.Background(), TypeControlYield, ControlYield{Command: "atomyx run login.yml"}, nil); err != nil {
		t.Fatalf("yield = %v, want ok before the connection ends", err)
	}
	select {
	case <-c.Done():
	case <-time.After(testGuard):
		t.Fatal("the connection never ended")
	}
}
