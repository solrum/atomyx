package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
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

func TestDeviceSession_AFreshCaptureIsRefusedWhileAToolCallIsInFlight(t *testing.T) {
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

	if release, ok := sess.gate.TryExclusive(); ok {
		release()
		t.Fatal("a fresh capture was admitted while a tool call was in flight")
	}

	close(finish)
	<-callDone
	release, ok := sess.gate.TryExclusive()
	if !ok {
		t.Fatal("a fresh capture was refused after the tool call ended")
	}
	release()
}

func errorResultCode(t *testing.T, result mcp.Result) string {
	t.Helper()
	toolResult, ok := result.(*mcp.CallToolResult)
	if !ok || !toolResult.IsError || len(toolResult.Content) != 1 {
		t.Fatalf("result = %+v, want one error content block", result)
	}
	var payload struct {
		Code string `json:"code"`
	}
	text := toolResult.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("result text %q is not a coded payload: %v", text, err)
	}
	return payload.Code
}

func TestToolCallMiddleware_TheWaitForTheGateCountsAgainstTheCallBudget(t *testing.T) {
	sess := newGateTestSession()
	t.Cleanup(sess.Close)
	capture, ok := sess.gate.TryExclusive()
	if !ok {
		t.Fatal("TryExclusive refused on a free gate")
	}
	defer capture()
	clk := clock.NewManualClock(0)
	ran := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		ran = true
		return &mcp.CallToolResult{}, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, clk, sess, sess, newToolTraits())(next)

	type outcome struct {
		result mcp.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := handler(context.Background(), toolCallMethod, toolCallRequest("tap"))
		done <- outcome{result, err}
	}()
	for sess.gate.Waiting() < 1 {
		runtime.Gosched()
	}
	clk.AwaitSleepers(1)
	clk.Advance(int64(defaultToolTimeoutMs))

	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if code := errorResultCode(t, got.result); code != diag.CodeToolTimeout {
		t.Fatalf("code = %q, want %q for a wait that outlasted the budget", code, diag.CodeToolTimeout)
	}
	if ran {
		t.Fatal("the tool ran although it never entered the gate")
	}
}

func TestToolCallMiddleware_ACallCanceledWhileItWaitsForTheGateIsNotReportedAsABusyHolder(t *testing.T) {
	sess := newGateTestSession()
	t.Cleanup(sess.Close)
	capture, _ := sess.gate.TryExclusive()
	defer capture()
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		t.Fatal("the tool ran although it never entered the gate")
		return nil, nil
	}
	handler := newToolCallMiddleware(logging.NoopLogger{}, clock.NewManualClock(0), sess, sess, newToolTraits())(next)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan mcp.Result, 1)
	go func() {
		result, _ := handler(ctx, toolCallMethod, toolCallRequest("tap"))
		done <- result
	}()
	for sess.gate.Waiting() < 1 {
		runtime.Gosched()
	}
	cancel()

	result := <-done
	if code := errorResultCode(t, result); code == diag.CodeHolderBusy {
		t.Fatalf("code = %q, a caller cancel is not a busy holder", code)
	}
	text := result.(*mcp.CallToolResult).Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, context.Canceled.Error()) {
		t.Fatalf("result %q does not report the cancellation", text)
	}
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
