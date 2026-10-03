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
	_, err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
	if got, _, _ := diag.Describe(err); got != diag.CodeUnsupported {
		t.Fatalf("code = %q (%v), want %s", got, err, diag.CodeUnsupported)
	}
}

func TestRequestYieldReturnsTheHoldersRefusal(t *testing.T) {
	control := &fakeControl{refuse: diag.NewHolderBusy("a run is open")}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	_, err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
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
	if _, err := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml"); err != nil {
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
	_, err := RequestYield(ctx, ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run login.yml")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context deadline", err)
	}
}

func TestRequestYieldResultSaysHowFarTheRequestGot(t *testing.T) {
	t.Run("a dial that fails sent nothing", func(t *testing.T) {
		result, err := RequestYield(context.Background(), ClientConfig{Path: t.TempDir() + "/missing.sock", Client: "borrower"}, "atomyx run")
		if err == nil || result.Sent || result.Acknowledged {
			t.Fatalf("result = %+v, %v, want an error with nothing sent", result, err)
		}
	})
	t.Run("a holder without control was not asked", func(t *testing.T) {
		srv := observeServer(t, newFakeObserve(), ServerConfig{})
		result, _ := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run")
		if result.Sent || result.Acknowledged {
			t.Fatalf("result = %+v, want nothing sent", result)
		}
	})
	t.Run("a refusal was sent and not acknowledged", func(t *testing.T) {
		srv := startServer(t, ServerConfig{Handlers: Handlers{Control: &fakeControl{refuse: diag.NewHolderBusy("busy")}}}, nil)
		result, _ := RequestYield(context.Background(), ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run")
		if !result.Sent || result.Acknowledged {
			t.Fatalf("result = %+v, want sent and not acknowledged", result)
		}
	})
	t.Run("an acknowledged request whose wait ends with the context", func(t *testing.T) {
		srv := startServer(t, ServerConfig{Handlers: Handlers{Control: &fakeControl{}}}, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		result, err := RequestYield(ctx, ClientConfig{Path: srv.Path(), Client: "borrower"}, "atomyx run")
		if !errors.Is(err, context.DeadlineExceeded) || !result.Sent || !result.Acknowledged {
			t.Fatalf("result = %+v, %v, want acknowledged before the deadline", result, err)
		}
	})
}
