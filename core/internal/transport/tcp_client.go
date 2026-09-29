package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"dev.atomyx/internal/logging"
)

// Tuning knobs for Connect's dial-retry loop, which absorbs a
// cold-starting iOS agent instead of failing on the first refused or
// timed-out dial.
const (
	// tcpConnectRetryBudget bounds the total time Connect spends
	// retrying a failed dial, matching the window a cold-starting
	// XCUITest runner process needs to come up and start accepting
	// connections.
	tcpConnectRetryBudget = 30 * time.Second
	// tcpConnectRetryInterval is the spacing between dial attempts
	// while the agent has not yet started accepting connections.
	tcpConnectRetryInterval = 500 * time.Millisecond
)

// tcpWriteTimeout bounds a blocking conn.Write call inside Call when
// the caller's context carries no deadline of its own, so a wedged
// peer that stops draining its socket cannot hang Call forever.
const tcpWriteTimeout = 10 * time.Second

// tcpResponseTimeout bounds the wait for a response inside Call when
// the caller's context carries no deadline of its own, so an agent
// that accepts a request but never answers cannot hang Call forever.
// It mirrors tcpWriteTimeout so the write phase and the response-wait
// phase apply the same fallback bound.
const tcpResponseTimeout = 10 * time.Second

// TCPClient speaks the iOS agent's line-delimited JSON protocol:
//
//	sent:     {"id":<n>,"type":<command>,"args":<object>}\n
//	received: {"id":<n>,"ok":true,"data":<object>}\n
//	received: {"id":<n>,"ok":false,"error":<string>}\n
//
// Every request carries a monotonically increasing id that the
// response echoes back, so responses may arrive out of order. This
// envelope has no notion of a route registry: the command vocabulary
// (its "type" values and per-command argument shapes) is owned by the
// caller, not by this client, because the iOS agent exposes commands
// over a single TCP connection instead of the Android agent's
// method-plus-path HTTP routes.
//
// A line that fails to parse as JSON, or that carries no id, is
// dropped rather than surfaced as an error: the agent process is
// known to emit unrelated log lines on the same stream before it
// starts serving.
type TCPClient struct {
	addr string

	// responseTimeout bounds the response wait inside Call when the
	// caller's context carries no deadline of its own. It is set from
	// tcpResponseTimeout at construction and never changes afterward
	// except in a test that wants that fallback bound to fire quickly
	// rather than waiting out the production duration.
	responseTimeout time.Duration

	// logger receives one debug-level completion line per Call,
	// carrying the run id from ctx when Call's caller populated one.
	// Defaults to logging.NoopLogger{}, so a caller that never applies
	// WithTCPLogger builds a client that logs nothing.
	logger logging.Logger

	// writeMu serializes the blocking Write calls issued by concurrent
	// Call invocations so two in-flight requests never interleave their
	// bytes on the wire. It never guards conn itself. mu does that, exclusively.
	writeMu sync.Mutex

	// mu guards every field below, including conn. conn is read under
	// mu into a local snapshot before any blocking network operation;
	// no field access ever happens while a blocking I/O call is in
	// flight, and no code outside this file ever reads conn without
	// holding mu.
	mu sync.Mutex

	conn net.Conn
	// generation increments on every successful Connect and every
	// Close. A readLoop or a Call's write-error path captures the
	// generation active when it started; handleDisconnect only acts on
	// a generation that is still current, so a stale goroutine racing
	// against a newer connection can never clobber that connection's
	// state.
	generation int64
	nextID     int64
	pending    map[int64]chan tcpResult
	dead       bool
}

type tcpResult struct {
	data json.RawMessage
	err  error
}

type tcpRequest struct {
	ID   int64          `json:"id"`
	Type string         `json:"type"`
	Args map[string]any `json:"args"`
}

type tcpResponse struct {
	ID    *int64          `json:"id"`
	Ok    *bool           `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *string         `json:"error"`
}

// TCPErrorCode classifies a TCPError the way the agent's own error
// vocabulary does, so callers can branch on failure kind without
// string matching.
type TCPErrorCode string

const (
	// TCPErrNotConnected means Call fired before Connect succeeded,
	// or after the connection was lost or closed.
	TCPErrNotConnected TCPErrorCode = "not-connected"
	// TCPErrDriverError means the agent answered the request with
	// ok:false.
	TCPErrDriverError TCPErrorCode = "driver-error"
	// TCPErrDisconnected means the socket closed, or Close was
	// called, while a request was in flight.
	TCPErrDisconnected TCPErrorCode = "disconnected"
	// TCPErrWriteTimeout means a request write blocked past the
	// fallback bound Call applies when the caller's context carries
	// no deadline of its own, meaning the peer stopped draining its
	// socket rather than the caller simply running out of time.
	TCPErrWriteTimeout TCPErrorCode = "write-timeout"
	// TCPErrResponseTimeout means the wait for a response ran past the
	// fallback bound Call applies when the caller's context carries no
	// deadline of its own, meaning the agent accepted the request but
	// never answered it rather than the caller simply running out of
	// time.
	TCPErrResponseTimeout TCPErrorCode = "response-timeout"
)

// TCPError is the error type every TCPClient failure is reported as,
// except a canceled or expired context, which surfaces as ctx.Err().
type TCPError struct {
	Code        TCPErrorCode
	RequestType string
	Message     string
	Err         error
}

func (e *TCPError) Error() string {
	prefix := "transport: tcp"
	if e.RequestType != "" {
		prefix = fmt.Sprintf("%s call(%s)", prefix, e.RequestType)
	}
	switch {
	case e.Err != nil:
		return fmt.Sprintf("%s: %s: %v", prefix, e.Code, e.Err)
	case e.Message != "":
		return fmt.Sprintf("%s: %s: %s", prefix, e.Code, e.Message)
	default:
		return fmt.Sprintf("%s: %s", prefix, e.Code)
	}
}

func (e *TCPError) Unwrap() error {
	return e.Err
}

// TCPClientOption configures a TCPClient constructed by NewTCPClient.
type TCPClientOption func(*TCPClient)

// WithTCPLogger replaces the logger every Call logs a completion line
// through. The default is logging.NoopLogger{}, so a caller that never
// calls WithTCPLogger keeps building a client that logs nothing.
func WithTCPLogger(l logging.Logger) TCPClientOption {
	return func(c *TCPClient) {
		c.logger = l
	}
}

// WithTCPResponseTimeout overrides tcpResponseTimeout, the fallback
// bound Call applies to the wait for a response when the caller's
// context carries no deadline of its own. A production caller should
// rarely need this; it exists mainly so a test can force that
// fallback path to fire on a short, deterministic bound instead of
// waiting out the production duration.
func WithTCPResponseTimeout(d time.Duration) TCPClientOption {
	return func(c *TCPClient) {
		c.responseTimeout = d
	}
}

// NewTCPClient builds a client for the iOS agent listening at
// host:port. The connection is not opened until Connect is called.
func NewTCPClient(host string, port int, opts ...TCPClientOption) *TCPClient {
	c := &TCPClient{
		addr:            net.JoinHostPort(host, strconv.Itoa(port)),
		responseTimeout: tcpResponseTimeout,
		pending:         make(map[int64]chan tcpResult),
		dead:            true,
		logger:          logging.NoopLogger{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Connect dials the agent, retrying a refused or failed dial at
// tcpConnectRetryInterval spacing for up to tcpConnectRetryBudget so
// a cold-starting XCUITest runner has time to come up and start
// accepting connections without the caller having to poll Connect
// itself. The retry budget is bounded by ctx's own deadline too, when
// ctx carries one earlier than tcpConnectRetryBudget. Canceling ctx
// aborts the retry loop and Connect returns ctx.Err().
//
// Calling Connect while already connected replaces the connection
// cleanly instead of leaking it: the new socket is dialed first, the
// prior socket (if any) is closed only after the swap, its readLoop
// goroutine exits on the resulting read error, and any calls still
// pending on it are rejected with TCPErrDisconnected.
func (c *TCPClient) Connect(ctx context.Context) error {
	retryCtx, cancel := context.WithTimeout(ctx, tcpConnectRetryBudget)
	defer cancel()

	conn, err := c.dialWithRetry(retryCtx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &TCPError{Code: TCPErrNotConnected, Err: err}
	}

	c.mu.Lock()
	oldConn := c.conn
	oldPending := c.pending
	wasLive := !c.dead
	c.generation++
	gen := c.generation
	c.conn = conn
	c.dead = false
	c.pending = make(map[int64]chan tcpResult)
	c.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}
	if wasLive {
		c.rejectAll(oldPending, &TCPError{Code: TCPErrDisconnected, Message: "client reconnected"})
	}

	go c.readLoop(conn, gen)
	return nil
}

// dialWithRetry dials the agent once, then keeps retrying at
// tcpConnectRetryInterval spacing until either a dial succeeds or ctx
// is done. It sleeps by selecting on ctx.Done rather than calling
// time.Sleep, so a cancellation during the pause between attempts is
// noticed immediately instead of after the full interval elapses.
// The returned error is the last genuine dial failure seen (for
// example connection refused); it falls back to ctx.Err() only when
// ctx was done before any dial attempt produced one.
func (c *TCPClient) dialWithRetry(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer
	var lastErr error
	for {
		conn, err := dialer.DialContext(ctx, "tcp", c.addr)
		if err == nil {
			return conn, nil
		}
		if ctx.Err() == nil {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ctx.Err()
		case <-time.After(tcpConnectRetryInterval):
		}
	}
}

// IsConnected reports whether the client currently holds a live
// connection.
func (c *TCPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.dead
}

// Close tears down the connection and rejects every in-flight Call
// with a TCPError of code TCPErrDisconnected. Close is idempotent.
func (c *TCPClient) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	wasDead := c.dead
	c.dead = true
	c.generation++
	pending := c.pending
	c.pending = make(map[int64]chan tcpResult)
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if !wasDead {
		c.rejectAll(pending, &TCPError{Code: TCPErrDisconnected, Message: "client closed by caller"})
	}
	return nil
}

func (c *TCPClient) rejectAll(pending map[int64]chan tcpResult, err error) {
	for _, ch := range pending {
		ch <- tcpResult{err: err}
	}
}

// readLoop drains newline-delimited response frames until the
// connection closes or errors, then rejects every waiter still
// pending a response. gen is the generation conn was assigned when
// Connect dialed it; it lets handleDisconnect tell a genuinely dead
// connection apart from a superseded one that Connect has already
// replaced.
func (c *TCPClient) readLoop(conn net.Conn, gen int64) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			c.handleDisconnect(gen, err)
			return
		}
	}
}

func (c *TCPClient) handleLine(line string) {
	var frame tcpResponse
	if jsonErr := json.Unmarshal([]byte(line), &frame); jsonErr != nil {
		return
	}
	if frame.ID == nil {
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[*frame.ID]
	if ok {
		delete(c.pending, *frame.ID)
	}
	c.mu.Unlock()
	if !ok {
		return
	}

	if frame.Ok != nil && *frame.Ok {
		ch <- tcpResult{data: frame.Data}
		return
	}
	message := "agent returned ok:false"
	if frame.Error != nil {
		message = *frame.Error
	}
	ch <- tcpResult{err: &TCPError{Code: TCPErrDriverError, Message: message}}
}

// handleDisconnect tears down state for the connection identified by
// gen. It is a no-op when that generation has already been
// superseded by a later Connect or Close, so a write failure or read
// error on a since-replaced connection can never clobber the state
// of the connection that replaced it.
func (c *TCPClient) handleDisconnect(gen int64, reason error) {
	c.mu.Lock()
	if c.dead || gen != c.generation {
		c.mu.Unlock()
		return
	}
	c.dead = true
	c.conn = nil
	pending := c.pending
	c.pending = make(map[int64]chan tcpResult)
	c.mu.Unlock()

	c.rejectAll(pending, &TCPError{Code: TCPErrDisconnected, Err: reason})
}

// Call sends a request of the given command type and raw arguments,
// and returns the raw data payload of the matching ok:true response.
// Canceling ctx before a response arrives removes the pending waiter
// and returns ctx.Err(); the request may still have reached the
// agent. The request write itself is bounded by ctx's own deadline,
// or by tcpWriteTimeout when ctx carries none, so a peer that stops
// draining its socket cannot block Call past that bound. Whether ctx
// carried a deadline is what Call attributes a resulting write
// timeout to, not the timing of ctx.Err() itself, since that would
// race against the same deadline the write just failed against: a
// write timeout is reported as ctx.Err() (or context.DeadlineExceeded)
// when ctx carried a deadline, and as a TCPError of code
// TCPErrWriteTimeout when it did not, meaning the fallback bound
// fired on a wedged peer instead. Any other write failure, and any
// Close or connection loss concurrent with Call, always yields a
// TCPError of code TCPErrDisconnected (or TCPErrNotConnected if the
// client was never connected).
//
// The subsequent wait for a response is bounded the same way: ctx's
// own deadline governs when it has one, and only when it does not
// does the tcpResponseTimeout fallback apply, so an agent that
// accepts the request but never answers cannot hang Call past that
// bound either. A response timeout under the fallback is reported as
// a TCPError of code TCPErrResponseTimeout; canceling or expiring a
// ctx that carries its own deadline still reports ctx.Err(). Call
// never panics regardless of what else races against it.
//
// Both fallback-timeout outcomes (TCPErrWriteTimeout and
// TCPErrResponseTimeout) are additionally classified as a
// diag.CodeToolTimeout diagnostic, recoverable through diag.Describe
// from anywhere in the caller's error-handling chain. A connection
// that was never established or was lost mid-call (TCPErrNotConnected,
// TCPErrDisconnected) is classified as an *UnreachableError, and the
// agent answering ok:false (TCPErrDriverError) is classified as an
// *AgentError carrying the agent's own message. Every other failure
// keeps its existing classification (or lack of one).
//
// Every invocation logs one debug-level completion line through
// logWireCall, whichever way it ends.
func (c *TCPClient) Call(ctx context.Context, cmdType string, args map[string]any) (json.RawMessage, error) {
	start := time.Now()
	data, err := c.callInner(ctx, cmdType, args)
	err = classifyTCPError(err, cmdType)
	c.logWireCall(ctx, cmdType, err, time.Since(start))
	return data, err
}

// logWireCall emits one minimal structured debug line for a completed
// wire call: the command type, whether it succeeded, and how long it
// took. It never logs the request arguments or response payload. The
// run id is attached when ctx carries one, so a driver-layer failure
// can be tied back to the run that produced it.
func (c *TCPClient) logWireCall(ctx context.Context, cmdType string, err error, duration time.Duration) {
	logging.ChildFromContext(c.logger, ctx).Debug("driver.wireCall", logging.LogContext{
		"command":    cmdType,
		"ok":         err == nil,
		"durationMs": duration.Milliseconds(),
	})
}

// callInner is the wire call Call wraps: it sends the request and
// waits for the matching response, with no logging of its own.
func (c *TCPClient) callInner(ctx context.Context, cmdType string, args map[string]any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.dead || c.conn == nil {
		c.mu.Unlock()
		return nil, &TCPError{Code: TCPErrNotConnected, RequestType: cmdType}
	}
	conn := c.conn
	gen := c.generation
	c.nextID++
	id := c.nextID
	ch := make(chan tcpResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	payload, err := json.Marshal(tcpRequest{ID: id, Type: cmdType, Args: args})
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("transport: encode request %q: %w", cmdType, err)
	}
	payload = append(payload, '\n')

	deadline, hadDeadline := writeDeadline(ctx)
	c.writeMu.Lock()
	writeErr := conn.SetWriteDeadline(deadline)
	if writeErr == nil {
		_, writeErr = conn.Write(payload)
		_ = conn.SetWriteDeadline(time.Time{})
	}
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(id)
		c.handleDisconnect(gen, writeErr)
		return nil, classifyWriteError(writeErr, cmdType, hadDeadline, ctx.Err())
	}

	// respTimeout stays nil when ctx already carries its own deadline,
	// so the select below falls through to ctx.Done() in that case: a
	// nil channel is never ready, and ctx's own deadline is what
	// governs without being shortened.
	var respTimeout <-chan time.Time
	if !hadDeadline {
		timer := time.NewTimer(c.responseTimeout)
		defer timer.Stop()
		respTimeout = timer.C
	}

	select {
	case res := <-ch:
		return res.data, res.err
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-respTimeout:
		c.removePending(id)
		return nil, &TCPError{Code: TCPErrResponseTimeout, RequestType: cmdType}
	}
}

// CallInto is Call plus decoding the returned data payload into out.
// out is left untouched when the response carries no data.
func (c *TCPClient) CallInto(ctx context.Context, cmdType string, args map[string]any, out any) error {
	data, err := c.Call(ctx, cmdType, args)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// writeDeadline picks the deadline for the single blocking conn.Write
// call inside Call: ctx's own deadline when it has one, or a bounded
// default measured from now, so the write can never hang past
// whichever bound applies. The second return value reports which case
// applied, so a caller can attribute a resulting write timeout without
// re-deriving it from ctx a second time.
func writeDeadline(ctx context.Context) (deadline time.Time, hadCtxDeadline bool) {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline, true
	}
	return time.Now().Add(tcpWriteTimeout), false
}

// classifyWriteError decides what Call should report when conn.Write
// fails. hadCtxDeadline and ctxErr must reflect ctx as observed around
// the write: hadCtxDeadline says which deadline bounded the write, and
// ctxErr is ctx.Err() sampled after the write returned.
//
// A write timeout is attributed to hadCtxDeadline alone, never to
// whether ctxErr is already non-nil: the write's own deadline and
// ctx's Done channel are armed from the same instant, so waiting for
// ctxErr to latch would make the outcome depend on which one the
// runtime happens to schedule first. When ctx did carry a deadline,
// that deadline is what bounded the write, so the caller's own
// deadline is reported (ctxErr when it has already latched, otherwise
// the equivalent sentinel). When ctx carried none, the fallback bound
// fired on a peer that stopped draining its socket, which is a
// distinct diagnosis from the caller's deadline and from a disconnect.
func classifyWriteError(writeErr error, cmdType string, hadCtxDeadline bool, ctxErr error) error {
	if !isWriteTimeoutError(writeErr) {
		return &TCPError{Code: TCPErrDisconnected, RequestType: cmdType, Err: writeErr}
	}
	if hadCtxDeadline {
		if ctxErr != nil {
			return ctxErr
		}
		return context.DeadlineExceeded
	}
	return &TCPError{Code: TCPErrWriteTimeout, RequestType: cmdType, Err: writeErr}
}

// isWriteTimeoutError reports whether err is the deadline-exceeded
// failure conn.Write returns after SetWriteDeadline fires, as opposed
// to a genuine disconnect (broken pipe, connection reset, closed
// connection).
func isWriteTimeoutError(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (c *TCPClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
