package mcp

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recordingGate struct {
	events *[]string
	err    error
}

func (g recordingGate) EnterCall(context.Context) (func(), error) {
	if g.err != nil {
		return nil, g.err
	}
	*g.events = append(*g.events, "enter")
	return func() { *g.events = append(*g.events, "release") }, nil
}

func toolCallRequest(name string) mcp.Request {
	return &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: name}}
}

func newGateTestSession() *DeviceSession {
	return NewDeviceSession(DeviceSessionDeps{Factory: device.NewMockDriverFactory()})
}

func TestToolCallMiddleware_DeviceDrivingCallsEnterTheGateForTheWholeCall(t *testing.T) {
	var events []string
	traits := newToolTraits()
	traits.observeOnly["check_health"] = true
	sess := newGateTestSession()
	t.Cleanup(sess.Close)
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		events = append(events, "call")
		return &mcp.CallToolResult{}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, clock.SystemClock{}, sess, recordingGate{events: &events}, traits)(next)

	if _, err := handler(context.Background(), toolCallMethod, toolCallRequest("tap")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"enter", "call", "release"}; !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}

	events = nil
	if _, err := handler(context.Background(), toolCallMethod, toolCallRequest("check_health")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"call"}; !equalStrings(events, want) {
		t.Fatalf("observe-only events = %v, want %v", events, want)
	}
}

func TestToolCallMiddleware_ACallThatCannotEnterTheGateIsRefusedWithoutRunning(t *testing.T) {
	var events []string
	sess := newGateTestSession()
	t.Cleanup(sess.Close)
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		events = append(events, "call")
		return &mcp.CallToolResult{}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, clock.SystemClock{}, sess, recordingGate{events: &events, err: errors.New("gate refused")}, newToolTraits())(next)

	result, err := handler(context.Background(), toolCallMethod, toolCallRequest("tap"))
	if err != nil {
		t.Fatal(err)
	}
	if toolResult, ok := result.(*mcp.CallToolResult); !ok || !toolResult.IsError {
		t.Fatalf("result = %+v, want an error result", result)
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want the call never to run", events)
	}
}

func TestDeviceSession_AFreshCaptureWaitsForAToolCallInFlight(t *testing.T) {
	sess := newGateTestSession()
	t.Cleanup(sess.Close)
	inCall := make(chan struct{})
	finish := make(chan struct{})
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		close(inCall)
		<-finish
		return &mcp.CallToolResult{}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, clock.SystemClock{}, sess, sess, newToolTraits())(next)
	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		_, _ = handler(context.Background(), toolCallMethod, toolCallRequest("tap"))
	}()
	<-inCall

	captured := make(chan struct{})
	go func() {
		release, err := sess.gate.Exclusive(context.Background())
		if err == nil {
			release()
		}
		close(captured)
	}()
	for sess.gate.Waiting() < 1 {
		runtime.Gosched()
	}
	select {
	case <-captured:
		t.Fatal("a fresh capture ran while a tool call was in flight")
	default:
	}

	close(finish)
	<-callDone
	<-captured
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
