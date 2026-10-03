package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolCallSocketGuard = 5 * time.Second

// blockingCall holds a tool call open inside the middleware until released.
type blockingCall struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	done    chan struct{}
}

func (c *blockingCall) finish() { c.once.Do(func() { close(c.release) }) }

func (c *blockingCall) await(t *testing.T) {
	t.Helper()
	c.finish()
	select {
	case <-c.done:
	case <-time.After(toolCallSocketGuard):
		t.Fatal("the blocked call did not return after it was released")
	}
}

func TestToolCallMiddleware_ASocketFreshCaptureIsRefusedDuringADrivingCallAndAdmittedDuringAnObserveOnlyCall(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	viewer := f.viewer("atomyx inspect")
	traits := newToolTraits()
	traits.observeOnly["check_health"] = true

	var current *blockingCall
	var mu sync.Mutex
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		mu.Lock()
		call := current
		mu.Unlock()
		call.entered <- struct{}{}
		<-call.release
		return &mcp.CallToolResult{}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, f.clk, f.devices, f.devices, traits)(next)
	begin := func(tool string) *blockingCall {
		call := &blockingCall{entered: make(chan struct{}, 1), release: make(chan struct{}), done: make(chan struct{})}
		mu.Lock()
		current = call
		mu.Unlock()
		t.Cleanup(call.finish)
		go func() {
			defer close(call.done)
			_, _ = handler(context.Background(), toolCallMethod, toolCallRequest(tool))
		}()
		select {
		case <-call.entered:
		case <-time.After(toolCallSocketGuard):
			t.Fatalf("the %s call never reached its handler", tool)
		}
		return call
	}

	driving := begin("tap")
	if got := codeOfErr(viewer.TreeFresh()); got != diag.CodeHolderBusy {
		t.Fatalf("fresh during a driving call = %q, want %s", got, diag.CodeHolderBusy)
	}
	driving.await(t)

	observing := begin("check_health")
	if err := viewer.TreeFresh(); err != nil {
		t.Fatalf("fresh during an observe-only call: %v, want it admitted because the call does not take the gate", err)
	}
	observing.await(t)
}
