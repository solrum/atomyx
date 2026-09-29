package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
)

// recordedLog is one Debug call a recordingLogger captured, with its
// own context merged over whatever context the Child chain that
// produced the logger carried.
type recordedLog struct {
	msg string
	ctx logging.LogContext
}

// recordingLogger records every Debug call, merging Child's context
// down the chain the way a real logging.Logger implementation does,
// so a test can assert a specific field (for example "runId") reached
// a logged entry through Child rather than being dropped.
type recordingLogger struct {
	ctx  logging.LogContext
	logs *[]recordedLog
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{logs: &[]recordedLog{}}
}

func (r *recordingLogger) Debug(msg string, ctx logging.LogContext) {
	*r.logs = append(*r.logs, recordedLog{msg: msg, ctx: mergeLogContext(r.ctx, ctx)})
}
func (r *recordingLogger) Info(string, logging.LogContext)         {}
func (r *recordingLogger) Warn(string, logging.LogContext)         {}
func (r *recordingLogger) Error(string, error, logging.LogContext) {}
func (r *recordingLogger) Child(ctx logging.LogContext) logging.Logger {
	return &recordingLogger{ctx: mergeLogContext(r.ctx, ctx), logs: r.logs}
}

func mergeLogContext(base, overlay logging.LogContext) logging.LogContext {
	merged := make(logging.LogContext, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

// fakeIosAgent is a minimal net.Listener-backed stand-in for the
// Swift XCUITest runner's TCP server: it accepts one connection,
// reads newline-delimited request frames, and answers each according
// to respond (or, when handleRequest is set, hands the raw request
// and connection to the test so it can control response timing
// itself). onConnect, when set, runs once right after the connection
// is accepted, before any request is read. Tests use it to write
// raw bytes onto the wire ahead of a real request.
//
// Every field must be set before calling start, because serve reads
// them without further synchronization; the goroutine boundary at
// the "go" statement in start is what makes that safe.
type fakeIosAgent struct {
	listener      net.Listener
	respond       func(reqType string, args map[string]any) (data any, agentErr string, ok bool)
	handleRequest func(conn net.Conn, req tcpRequest)
	onConnect     func(conn net.Conn)
}

func newFakeIosAgent(t *testing.T) *fakeIosAgent {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	agent := &fakeIosAgent{listener: listener}
	t.Cleanup(func() { _ = listener.Close() })
	return agent
}

func (a *fakeIosAgent) start() {
	go a.serve()
}

func startFakeIosAgent(t *testing.T, respond func(reqType string, args map[string]any) (data any, agentErr string, ok bool)) *fakeIosAgent {
	agent := newFakeIosAgent(t)
	agent.respond = respond
	agent.start()
	return agent
}

func (a *fakeIosAgent) serve() {
	conn, err := a.listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if a.onConnect != nil {
		a.onConnect(conn)
	}

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			a.handleLine(conn, line)
		}
		if err != nil {
			return
		}
	}
}

func (a *fakeIosAgent) handleLine(conn net.Conn, line string) {
	var req tcpRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return
	}
	if a.handleRequest != nil {
		a.handleRequest(conn, req)
		return
	}
	data, agentErr, ok := a.respond(req.Type, req.Args)
	var resp map[string]any
	if ok {
		resp = map[string]any{"id": req.ID, "ok": true, "data": data}
	} else {
		resp = map[string]any{"id": req.ID, "ok": false, "error": agentErr}
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(encoded, '\n'))
}

func (a *fakeIosAgent) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(a.listener.Addr().String())
	if err != nil {
		t.Fatalf("split fake agent address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse fake agent port: %v", err)
	}
	return host, port
}

func TestTCPClient_Call_SendsFrameAndDecodesData(t *testing.T) {
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		if reqType != "tapAt" {
			return nil, fmt.Sprintf("unexpected type %q", reqType), false
		}
		return map[string]any{"x": args["x"], "y": args["y"]}, "", true
	})
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	var out struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := client.CallInto(ctx, "tapAt", map[string]any{"x": 10.0, "y": 20.0}, &out); err != nil {
		t.Fatalf("CallInto() error = %v", err)
	}
	if out.X != 10 || out.Y != 20 {
		t.Fatalf("CallInto() decoded %+v, want x=10 y=20", out)
	}
}

func TestTCPClient_Call_DriverErrorResponse(t *testing.T) {
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		return nil, "clear-text-failed", false
	})
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err := client.Call(ctx, "clearFocusedInput", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want a TCPError")
	}
	var tcpErr *TCPError
	if !errors.As(err, &tcpErr) {
		t.Fatalf("Call() error = %v (%T), want *TCPError", err, err)
	}
	if tcpErr.Code != TCPErrDriverError || tcpErr.Message != "clear-text-failed" {
		t.Fatalf("TCPError = %+v, want code driver-error message clear-text-failed", tcpErr)
	}
}

// TestTCPClient_Call_DriverErrorResponse_ClassifiesAsAgentError proves
// that the agent answering ok:false classifies as an *AgentError
// carrying the agent's own message, and never as IsUnreachable: the
// connection is fine, the agent simply reported the action failed.
func TestTCPClient_Call_DriverErrorResponse_ClassifiesAsAgentError(t *testing.T) {
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		return nil, "clear-text-failed", false
	})
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err := client.Call(ctx, "clearFocusedInput", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want an AgentError")
	}
	if IsUnreachable(err) {
		t.Fatalf("Call() error = %v, want IsUnreachable false for an agent-reported failure", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("Call() error = %v (%T), want *AgentError", err, err)
	}
	if agentErr.Message != "clear-text-failed" {
		t.Fatalf("AgentError.Message = %q, want %q", agentErr.Message, "clear-text-failed")
	}
}

func TestTCPClient_Call_BeforeConnect_ReturnsNotConnected(t *testing.T) {
	client := NewTCPClient("127.0.0.1", 1)
	_, err := client.Call(context.Background(), "ping", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want a TCPError")
	}
	var tcpErr *TCPError
	if !errors.As(err, &tcpErr) || tcpErr.Code != TCPErrNotConnected {
		t.Fatalf("Call() error = %v, want TCPErrNotConnected", err)
	}
	if !IsUnreachable(err) {
		t.Fatalf("Call() error = %v, want IsUnreachable true before any connection was ever established", err)
	}
}

// TestTCPClient_Call_ClosedListener_ClassifiesAsUnreachable proves
// that a dead endpoint (nothing listening on the port at all)
// classifies as IsUnreachable true, matching the HTTP client's own
// classification of the same failure shape.
func TestTCPClient_Call_ClosedListener_ClassifiesAsUnreachable(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse listener port: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	client := NewTCPClient(host, port)
	_, callErr := client.Call(context.Background(), "ping", nil)
	if callErr == nil {
		t.Fatal("Call() error = nil, want a TCPError")
	}
	if !IsUnreachable(callErr) {
		t.Fatalf("Call() error = %v, want IsUnreachable true against a dead endpoint", callErr)
	}
}

func TestTCPClient_Call_ContextCanceled_AbortsWait(t *testing.T) {
	release := make(chan struct{})
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		<-release
		return map[string]any{}, "", true
	})
	host, port := agent.hostPort(t)
	defer close(release)

	client := NewTCPClient(host, port)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.Call(ctx, "ping", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
}

func TestTCPClient_Call_MalformedAndIdlessLinesDropped(t *testing.T) {
	agent := newFakeIosAgent(t)
	agent.respond = func(reqType string, args map[string]any) (any, string, bool) {
		return map[string]any{"echo": reqType}, "", true
	}
	agent.onConnect = func(conn net.Conn) {
		// Neither line carries a usable id: the first fails to parse
		// as JSON, the second parses but has no "id" field.
		_, _ = conn.Write([]byte("not json at all\n"))
		_, _ = conn.Write([]byte(`{"ok":true,"data":{}}` + "\n"))
	}
	agent.start()
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	var out struct {
		Echo string `json:"echo"`
	}
	if err := client.CallInto(ctx, "ping", nil, &out); err != nil {
		t.Fatalf("CallInto() error = %v", err)
	}
	if out.Echo != "ping" {
		t.Fatalf("CallInto() decoded %+v, want echo=ping", out)
	}
}

func TestTCPClient_Call_OutOfOrderResponses_CorrelateById(t *testing.T) {
	type pendingReq struct {
		id   int64
		conn net.Conn
	}
	release := make(chan pendingReq, 2)
	agent := newFakeIosAgent(t)
	agent.handleRequest = func(conn net.Conn, req tcpRequest) {
		release <- pendingReq{id: req.ID, conn: conn}
	}
	agent.start()
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	type callResult struct {
		label string
		data  json.RawMessage
		err   error
	}
	results := make(chan callResult, 2)
	go func() {
		data, err := client.Call(ctx, "first", nil)
		results <- callResult{label: "first", data: data, err: err}
	}()
	// The second Call only starts once the first has already reached
	// the fake agent, so the two requests arrive in a known order
	// while still overlapping: both are outstanding when the
	// responses below are sent back in reverse.
	first := <-release
	go func() {
		data, err := client.Call(ctx, "second", nil)
		results <- callResult{label: "second", data: data, err: err}
	}()
	second := <-release

	respond := func(p pendingReq, value string) {
		encoded, err := json.Marshal(map[string]any{"id": p.id, "ok": true, "data": map[string]any{"who": value}})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		if _, err := p.conn.Write(append(encoded, '\n')); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}
	// Answer in reverse order: whichever Call reached the fake agent
	// second gets its response first.
	respond(second, "second-reply")
	respond(first, "first-reply")

	got := map[string]string{}
	for i := 0; i < 2; i++ {
		res := <-results
		if res.err != nil {
			t.Fatalf("Call(%s) error = %v", res.label, res.err)
		}
		var decoded struct {
			Who string `json:"who"`
		}
		if err := json.Unmarshal(res.data, &decoded); err != nil {
			t.Fatalf("decode Call(%s) result: %v", res.label, err)
		}
		got[res.label] = decoded.Who
	}
	if got["first"] != "first-reply" || got["second"] != "second-reply" {
		t.Fatalf("Call results = %+v, want first=first-reply second=second-reply", got)
	}
}

func TestTCPClient_ConcurrentCloseDuringCall_NeverPanicsAndReportsDisconnected(t *testing.T) {
	release := make(chan struct{})
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		<-release
		return map[string]any{}, "", true
	})
	host, port := agent.hostPort(t)
	defer close(release)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Call() panicked: %v", r)
				}
			}()
			_, err := client.Call(ctx, "ping", nil)
			if err == nil {
				return
			}
			var tcpErr *TCPError
			if !errors.As(err, &tcpErr) {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("Call() error = %v (%T), want *TCPError or ctx error", err, err)
				}
				return
			}
			if tcpErr.Code != TCPErrDisconnected && tcpErr.Code != TCPErrNotConnected {
				t.Errorf("Call() error code = %v, want disconnected or not-connected", tcpErr.Code)
			}
		}()
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	wg.Wait()

	if _, err := client.Call(context.Background(), "ping", nil); err == nil {
		t.Fatal("Call() after Close() error = nil, want TCPErrNotConnected")
	}
}

// TestTCPClient_Call_WriteDeadline_BoundsBlockedWrite proves that a
// wedged peer that never drains its socket cannot hang Call: the
// write deadline derives from ctx's own deadline, so Call fails
// quickly instead of blocking past it. serverConn is never read from,
// so clientConn's Write blocks until the deadline set on it expires.
// The outcome is asserted as context.DeadlineExceeded regardless of
// whether ctx.Err() has latched by the time Call inspects it, because
// Call attributes the write timeout to ctx's own deadline deterministically
// from the fact that ctx carried one, not from that latch timing.
func TestTCPClient_Call_WriteDeadline_BoundsBlockedWrite(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	client := NewTCPClient("127.0.0.1", 0)
	client.mu.Lock()
	client.conn = clientConn
	client.dead = false
	client.generation++
	client.mu.Unlock()

	const budget = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	start := time.Now()
	_, err := client.Call(ctx, "ping", nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want context.DeadlineExceeded because the write deadline derives from ctx", err)
	}
	const slack = 2 * time.Second
	if elapsed > budget+slack {
		t.Fatalf("Call() took %v to fail against a %v ctx budget, want the blocked write bounded by it rather than hanging", elapsed, budget)
	}
}

// TestTCPClient_Call_ResponseTimeout_BoundsUnansweredRequest proves
// that an agent that accepts a request but never answers it cannot
// hang Call forever when ctx carries no deadline of its own: the
// response wait falls back to the client's response timeout, and
// Call returns a typed TCPErrResponseTimeout, recoverable through
// diag.Describe as diag.CodeToolTimeout, instead of blocking. The
// client's response timeout is shortened before the call so the test
// does not have to wait out the production duration to observe the
// same fallback path firing.
func TestTCPClient_Call_ResponseTimeout_BoundsUnansweredRequest(t *testing.T) {
	agent := newFakeIosAgent(t)
	agent.handleRequest = func(conn net.Conn, req tcpRequest) {
		// Read and accept the request, then answer nothing.
	}
	agent.start()
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	const budget = 100 * time.Millisecond
	client.responseTimeout = budget

	start := time.Now()
	_, err := client.Call(context.Background(), "ping", nil)
	elapsed := time.Since(start)

	var tcpErr *TCPError
	if !errors.As(err, &tcpErr) || tcpErr.Code != TCPErrResponseTimeout {
		t.Fatalf("Call() error = %v, want a TCPError of code %q", err, TCPErrResponseTimeout)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("diag.Describe(Call() error) code = %q, want %q", code, diag.CodeToolTimeout)
	}
	const slack = 2 * time.Second
	if elapsed > budget+slack {
		t.Fatalf("Call() took %v to fail against a %v response timeout, want the unanswered wait bounded by it rather than hanging", elapsed, budget)
	}
}

// TestTCPClient_Call_LogsWireCallWithRunID proves that a completed
// Call logs a driver.wireCall debug entry carrying the run id a
// caller populated onto ctx via logging.WithRunID, so a driver-layer
// log line can be tied back to the run that produced it.
func TestTCPClient_Call_LogsWireCallWithRunID(t *testing.T) {
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		return map[string]any{}, "", true
	})
	host, port := agent.hostPort(t)

	logger := newRecordingLogger()
	client := NewTCPClient(host, port, WithTCPLogger(logger))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	runCtx := logging.WithRunID(ctx, "run-123")
	if _, err := client.Call(runCtx, "ping", nil); err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	logs := *logger.logs
	if len(logs) != 1 {
		t.Fatalf("logs = %d entries, want 1: %+v", len(logs), logs)
	}
	entry := logs[0]
	if entry.msg != "driver.wireCall" {
		t.Fatalf("log msg = %q, want driver.wireCall", entry.msg)
	}
	if got := entry.ctx["runId"]; got != "run-123" {
		t.Fatalf("log runId field = %v, want run-123", got)
	}
	if got := entry.ctx["command"]; got != "ping" {
		t.Fatalf("log command field = %v, want ping", got)
	}
	if ok, _ := entry.ctx["ok"].(bool); !ok {
		t.Fatalf("log ok field = %v, want true", entry.ctx["ok"])
	}
}

// fakeTimeoutErr is a net.Error stand-in for the deadline-exceeded
// failure conn.Write returns after SetWriteDeadline fires, so
// classifyWriteError can be tested without a real blocked write.
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string   { return "fake i/o timeout" }
func (fakeTimeoutErr) Timeout() bool   { return true }
func (fakeTimeoutErr) Temporary() bool { return false }

// TestClassifyWriteError covers both branches of a write timeout
// without waiting for a real deadline. A context deadline present
// around the write means the caller's own deadline bounded it, so the
// caller's deadline error is what Call surfaces. A context deadline
// absent means the fallback bound fired on a peer that stopped
// draining its socket, so a distinct typed transport error is what
// Call surfaces instead. A non-timeout write error keeps the existing
// disconnected-transport diagnosis.
func TestClassifyWriteError(t *testing.T) {
	writeTimeout := fakeTimeoutErr{}
	disconnectErr := errors.New("broken pipe")

	t.Run("timeout with context deadline not yet latched yields context.DeadlineExceeded", func(t *testing.T) {
		err := classifyWriteError(writeTimeout, "ping", true, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("classifyWriteError() = %v, want context.DeadlineExceeded", err)
		}
	})

	t.Run("timeout with context deadline already latched yields ctx.Err()", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := classifyWriteError(writeTimeout, "ping", true, ctx.Err())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("classifyWriteError() = %v, want the latched ctx.Err()", err)
		}
	})

	t.Run("timeout without context deadline yields TCPErrWriteTimeout", func(t *testing.T) {
		err := classifyWriteError(writeTimeout, "ping", false, nil)
		var tcpErr *TCPError
		if !errors.As(err, &tcpErr) || tcpErr.Code != TCPErrWriteTimeout {
			t.Fatalf("classifyWriteError() = %v, want a TCPError of code %q", err, TCPErrWriteTimeout)
		}
		if tcpErr.RequestType != "ping" {
			t.Fatalf("classifyWriteError() RequestType = %q, want %q", tcpErr.RequestType, "ping")
		}
	})

	t.Run("non-timeout write error yields TCPErrDisconnected regardless of context deadline", func(t *testing.T) {
		err := classifyWriteError(disconnectErr, "ping", true, nil)
		var tcpErr *TCPError
		if !errors.As(err, &tcpErr) || tcpErr.Code != TCPErrDisconnected {
			t.Fatalf("classifyWriteError() = %v, want a TCPError of code %q", err, TCPErrDisconnected)
		}
	})
}

func TestTCPClient_Connect_ContextCanceled(t *testing.T) {
	// A non-routable address (RFC 5737 TEST-NET-1) that never
	// completes a handshake, so canceling ctx is what ends the dial.
	client := NewTCPClient("192.0.2.1", 22087)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		t.Fatal("Connect() error = nil, want a timeout or context error")
	}
}

// reserveFreeTCPAddr finds a currently-free localhost TCP port by
// briefly binding to port 0 and releasing it, so a test can dial an
// address before anything is listening on it without hardcoding a
// port number that might already be in use.
func reserveFreeTCPAddr(t *testing.T) (host string, port int, addr string) {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr = listener.Addr().String()
	if closeErr := listener.Close(); closeErr != nil {
		t.Fatalf("release reserved port: %v", closeErr)
	}
	hostStr, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split reserved address: %v", err)
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse reserved port: %v", err)
	}
	return hostStr, portNum, addr
}

func TestTCPClient_Connect_SucceedsImmediatelyWhenAgentIsAlreadyListening(t *testing.T) {
	agent := startFakeIosAgent(t, func(reqType string, args map[string]any) (any, string, bool) {
		return map[string]any{}, "", true
	})
	host, port := agent.hostPort(t)

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()
	if elapsed := time.Since(start); elapsed >= tcpConnectRetryInterval {
		t.Fatalf("Connect() took %v against an already-listening agent, want well under one retry interval (%v)", elapsed, tcpConnectRetryInterval)
	}
}

func TestTCPClient_Connect_RetriesUntilAColdStartingAgentStartsListening(t *testing.T) {
	host, port, addr := reserveFreeTCPAddr(t)

	// The agent starts listening only after coldStartDelay, which is
	// shorter than tcpConnectRetryInterval: the first dial attempt
	// must fail (nothing listening yet) and Connect must retry rather
	// than giving up, for the second attempt to find it up.
	const coldStartDelay = 150 * time.Millisecond
	type listenResult struct {
		listener net.Listener
		err      error
	}
	ready := make(chan listenResult, 1)
	go func() {
		time.Sleep(coldStartDelay)
		var lc net.ListenConfig
		listener, err := lc.Listen(context.Background(), "tcp", addr)
		ready <- listenResult{listener: listener, err: err}
		if err == nil {
			agent := &fakeIosAgent{listener: listener, respond: func(reqType string, args map[string]any) (any, string, bool) {
				return map[string]any{}, "", true
			}}
			agent.start()
		}
	}()

	client := NewTCPClient(host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want it to retry past the cold-start window and succeed", err)
	}
	defer func() { _ = client.Close() }()

	result := <-ready
	if result.err != nil {
		t.Fatalf("cold-start listener failed to start: %v", result.err)
	}
	t.Cleanup(func() { _ = result.listener.Close() })
}

func TestTCPClient_Connect_GivesUpWhenCtxDeadlineExpiresBeforeAnyListenerAppears(t *testing.T) {
	host, port, _ := reserveFreeTCPAddr(t)

	client := NewTCPClient(host, port)
	const budget = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	start := time.Now()
	err := client.Connect(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connect() error = %v, want context.DeadlineExceeded", err)
	}
	const slack = 500 * time.Millisecond
	if elapsed > budget+slack {
		t.Fatalf("Connect() took %v to give up against a %v ctx budget, want it bounded by the shorter ctx deadline rather than the full %v retry budget", elapsed, budget, tcpConnectRetryBudget)
	}
}
