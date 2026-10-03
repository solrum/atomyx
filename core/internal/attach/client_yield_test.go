package attach

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

func TestRequestYieldToAHolderWithoutControlIsUnsupported(t *testing.T) {
	srv := observeServer(t, newFakeObserve(), ServerConfig{})
	err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
	if got, _, _ := diag.Describe(err); got != diag.CodeUnsupported {
		t.Fatalf("code = %q (%v), want %s", got, err, diag.CodeUnsupported)
	}
}

func TestRequestYieldReturnsTheHoldersRefusal(t *testing.T) {
	control := &fakeControl{refuse: diag.NewHolderBusy("a run is open")}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code() != diag.CodeHolderBusy {
		t.Fatalf("err = %v, want a remote %s", err, diag.CodeHolderBusy)
	}
}

func TestRequestYieldReturnsOnceTheHolderClosesAfterItsReply(t *testing.T) {
	var srv *Server
	control := &fakeControl{borrows: make(chan string, 1)}
	control.after = func() { _ = srv.Close() }
	srv = startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	if err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml"); err != nil {
		t.Fatalf("yield = %v", err)
	}
	if got := <-control.borrows; got != "atomyx run login.yml" {
		t.Fatalf("borrower = %q", got)
	}
}

func TestRequestYieldHonorsTheContextWhileTheHolderKeepsTheConnection(t *testing.T) {
	control := &fakeControl{}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := RequestYield(ctx, ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context deadline", err)
	}
}
