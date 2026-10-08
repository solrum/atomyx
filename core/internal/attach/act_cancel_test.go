package attach

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

// blockingAct is a debug handler whose act runs until its context ends.
type blockingAct struct {
	fakeDebug
	entered chan struct{}
	ended   chan error
}

func (b *blockingAct) Act(ctx context.Context, _ string) (ActResult, error) {
	b.entered <- struct{}{}
	<-ctx.Done()
	b.ended <- ctx.Err()
	return ActResult{}, ctx.Err()
}

func newBlockingAct() *blockingAct {
	return &blockingAct{entered: make(chan struct{}, 2), ended: make(chan error, 2)}
}

func TestAnActIsCancelledWhenItsViewerDisconnects(t *testing.T) {
	h := newBlockingAct()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	client.send(TypeActStep, ActStep{Step: "- tap: Login"})
	<-h.entered

	_ = client.nc.Close()
	select {
	case err := <-h.ended:
		if err == nil {
			t.Fatal("the act ended without its context being cancelled")
		}
	case <-time.After(testGuard):
		t.Fatal("the act kept running after its viewer disconnected")
	}
}

func TestASecondActOnTheSameConnectionIsRefusedWhileOneRuns(t *testing.T) {
	h := newBlockingAct()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Debug: h}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	client.send(TypeActStep, ActStep{Step: "- tap: Login"})
	<-h.entered

	if got := failureCode(t, client.call(TypeActStep, ActStep{Step: "- tap: Again"})); got != diag.CodeHolderBusy {
		t.Fatalf("code = %q, want %s", got, diag.CodeHolderBusy)
	}
	if ok := client.call(TypeDebugPause, nil).Ok; ok == nil || !*ok {
		t.Fatal("a request during an act was not answered")
	}
}
