package attach

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

type fakeControl struct {
	refuse  error
	after   func()
	borrows chan string
}

func (f *fakeControl) Yield(_ context.Context, borrower string) (func(), error) {
	if f.borrows != nil {
		f.borrows <- borrower
	}
	if f.refuse != nil {
		return nil, f.refuse
	}
	return f.after, nil
}

func helloFeatures(t *testing.T, c *rawClient) []string {
	t.Helper()
	var result HelloResult
	if err := c.hello().Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Features
}

func TestYieldToAHolderWithoutAControlHandlerIsUnsupported(t *testing.T) {
	srv := startServer(t, ServerConfig{Handlers: Handlers{Session: fakeSession{Status{State: "idle"}}}}, nil)
	client := dialRaw(t, srv.Path())
	if slices.Contains(helloFeatures(t, client), FeatureControl) {
		t.Fatal("hello advertises control without a control handler")
	}
	reply := client.call(TypeControlYield, ControlYield{Command: "atomyx run login.yml"})
	if got := failureCode(t, reply); got != diag.CodeUnsupported {
		t.Fatalf("code = %q, want %s", got, diag.CodeUnsupported)
	}
	if ok := client.call(TypeStatus, nil).Ok; ok == nil || !*ok {
		t.Fatal("status after UNSUPPORTED failed")
	}
}

func TestHelloAdvertisesControlOnlyWithAControlHandler(t *testing.T) {
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: &fakeControl{}}}, nil)
	client := dialRaw(t, srv.Path())
	if !slices.Contains(helloFeatures(t, client), FeatureControl) {
		t.Fatal("hello lacks control although a control handler is set")
	}
}

func TestRefusedYieldIsHolderBusyAndTheConnectionStaysUsable(t *testing.T) {
	control := &fakeControl{refuse: diag.NewHolderBusy("a tool call is running"), borrows: make(chan string, 4)}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: control, Session: fakeSession{Status{State: "running"}}}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	for range 2 {
		reply := client.call(TypeControlYield, ControlYield{Command: "atomyx run login.yml"})
		if got := failureCode(t, reply); got != diag.CodeHolderBusy {
			t.Fatalf("code = %q, want %s", got, diag.CodeHolderBusy)
		}
	}
	if got := <-control.borrows; got != "atomyx run login.yml" {
		t.Fatalf("borrower = %q", got)
	}
	if ok := client.call(TypeStatus, nil).Ok; ok == nil || !*ok {
		t.Fatal("status after HOLDER_BUSY failed")
	}
}

func TestYieldWithAnInvalidCommandIsRefusedBeforeTheHandler(t *testing.T) {
	control := &fakeControl{borrows: make(chan string, 1)}
	srv := startServer(t, ServerConfig{Handlers: Handlers{Control: control}}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	for _, command := range []string{"", strings.Repeat("x", MaxBorrowerCommandBytes+1)} {
		if got := failureCode(t, client.call(TypeControlYield, ControlYield{Command: command})); got != diag.CodeInvalidArgument {
			t.Fatalf("code = %q, want %s", got, diag.CodeInvalidArgument)
		}
	}
	if len(control.borrows) != 0 {
		t.Fatal("the handler saw an invalid command")
	}
}

func TestAcceptedYieldRunsTheCallbackOnlyAfterTheReplyIsWritten(t *testing.T) {
	var writes atomic.Int32
	release := make(chan struct{})
	called := make(chan struct{})
	control := &fakeControl{after: func() { close(called) }}
	cfg := ServerConfig{Handlers: Handlers{Control: control}}
	cfg.hooks.beforeWrite = func() {
		if writes.Add(1) == 2 {
			<-release
		}
	}
	srv := startServer(t, cfg, nil)
	var releaseOnce sync.Once
	releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseWriter)
	client := dialRaw(t, srv.Path())
	client.hello()
	id := client.send(TypeControlYield, ControlYield{Command: "atomyx run login.yml"})

	select {
	case <-called:
		t.Fatal("the callback ran while the reply was still unwritten")
	case <-time.After(150 * time.Millisecond):
	}
	releaseWriter()
	if reply := client.read(); reply.ID != id || reply.Ok == nil || !*reply.Ok {
		t.Fatalf("reply = %+v, want ok for id %d", reply, id)
	}
	select {
	case <-called:
	case <-time.After(testGuard):
		t.Fatal("the callback never ran")
	}
}

func TestAcceptedYieldRunsTheCallbackWhenTheConnectionEndsFirst(t *testing.T) {
	called := make(chan struct{})
	control := &fakeControl{after: func() { close(called) }}
	cfg := ServerConfig{Handlers: Handlers{Control: control}}
	release := make(chan struct{})
	var writes atomic.Int32
	cfg.hooks.beforeWrite = func() {
		if writes.Add(1) == 2 {
			<-release
		}
	}
	srv := startServer(t, cfg, nil)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	client := dialRaw(t, srv.Path())
	client.hello()
	client.send(TypeControlYield, ControlYield{Command: "atomyx run login.yml"})
	_ = client.nc.Close()
	select {
	case <-called:
	case <-time.After(testGuard):
		t.Fatal("the callback never ran after the connection ended")
	}
}
