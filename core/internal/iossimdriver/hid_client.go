package iossimdriver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"dev.atomyx/internal/diag"
)

// NormalizedPoint is a screen point with x and y in [0..1], the
// coordinate space the atomyx-sim-hid helper's dispatch messages
// expect.
type NormalizedPoint struct {
	X float64
	Y float64
}

// HidClientErrorCode names one of the ways a HidClient call can fail,
// so a caller can branch on Code instead of matching Message text.
type HidClientErrorCode string

const (
	HidClientErrNotConnected HidClientErrorCode = "not-connected"
	HidClientErrTimeout      HidClientErrorCode = "timeout"
	HidClientErrReplyError   HidClientErrorCode = "reply-error"
	HidClientErrDisconnected HidClientErrorCode = "disconnected"
)

// HidClientError is a named, actionable failure from a HidClient
// call.
type HidClientError struct {
	Message string
	Code    HidClientErrorCode
}

func (e *HidClientError) Error() string { return e.Message }

// defaultDispatchTimeout bounds how long a dispatch waits for the
// helper's reply before failing with HidClientErrTimeout.
const defaultDispatchTimeout = 2 * time.Second

// hidDispatchTimeoutRemedy is the remedy text attached whenever a
// dispatch's own timeout fires, matching the guidance the transport
// package's wire-call fallback gives for the same kind of failure.
const hidDispatchTimeoutRemedy = "Confirm the simulator is still responsive, then retry."

// classifyDispatchTimeout wraps err, a dispatch's own timeout
// failure, as a diag.CodeToolTimeout diagnostic, consistent with the
// wire-call methods' own fallback-timeout classification. err stays
// reachable through errors.As for a caller that needs the underlying
// *HidClientError.
func classifyDispatchTimeout(err error) error {
	return diag.New(diag.CodeToolTimeout,
		"the simulator did not respond to the touch dispatch within its time budget",
		hidDispatchTimeoutRemedy, err)
}

// swipeStepMs and maxSwipeSteps convert a swipe's durationMs into the
// (steps, stepMs) pair the helper's swipe message expects: steps is
// durationMs divided by swipeStepMs, rounded to the nearest step and
// capped so a very long swipe still dispatches in one bounded call.
const (
	swipeStepMs   = 16
	maxSwipeSteps = 60
)

// defaultTapHoldMs is the hold duration HidClient.Tap uses when the
// caller does not need a specific hold time, matching a quick tap
// rather than a long-press gesture.
const defaultTapHoldMs = 50

// HidClientOptions configures a HidClient.
type HidClientOptions struct {
	// Port is the WS port the atomyx-sim-hid helper's handshake
	// reported.
	Port int
	// DispatchTimeout bounds a single dispatch's wait for a reply.
	// Zero means defaultDispatchTimeout.
	DispatchTimeout time.Duration
}

type pendingReply struct {
	replyCh chan error
}

// HidClient is a WebSocket client for the atomyx-sim-hid helper
// process.
//
// Protocol (helper-defined):
//
//	-> newline-terminated JSON gesture messages, one per WS text frame
//	<- newline-terminated JSON replies: {"ok":true|false,"id":N,"error":"..."}
//
// It performs the RFC 6455 opening handshake and frame codec itself
// over an injected Dialer, because the helper speaks raw WebSocket
// framing rather than HTTP.
type HidClient struct {
	dialer          Dialer
	port            int
	dispatchTimeout time.Duration

	mu      sync.Mutex
	conn    net.Conn
	dead    bool
	nextID  int
	pending map[int]pendingReply
}

// NewHidClient constructs a HidClient. Connect must be called before
// any dispatch method.
func NewHidClient(dialer Dialer, opts HidClientOptions) *HidClient {
	timeout := opts.DispatchTimeout
	if timeout <= 0 {
		timeout = defaultDispatchTimeout
	}
	return &HidClient{
		dialer:          dialer,
		port:            opts.Port,
		dispatchTimeout: timeout,
		dead:            true,
		nextID:          1,
		pending:         map[int]pendingReply{},
	}
}

// Connect opens the underlying connection and performs the WebSocket
// opening handshake. It returns once the helper has answered with a
// 101 Switching Protocols response; a background goroutine then reads
// and dispatches replies until the connection fails or Dispose is
// called.
func (c *HidClient) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	type result struct {
		conn     net.Conn
		leftover []byte
		err      error
	}
	resCh := make(chan result, 1)
	go func() {
		conn, leftover, err := c.dialAndHandshake(ctx)
		resCh <- result{conn, leftover, err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			return res.err
		}
		c.mu.Lock()
		c.conn = res.conn
		c.dead = false
		c.mu.Unlock()
		go c.readLoop(res.conn, res.leftover)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dialAndHandshake dials the helper and performs the RFC 6455 opening
// handshake. A watcher goroutine closes the freshly dialed connection
// if ctx is canceled mid-handshake, since net.Conn reads and writes
// carry no context of their own.
func (c *HidClient) dialAndHandshake(ctx context.Context) (net.Conn, []byte, error) {
	conn, err := c.dialer.Dial(ctx, c.port)
	if err != nil {
		return nil, nil, &HidClientError{
			Message: fmt.Sprintf("hid-client: WS connect failed: %v", err),
			Code:    HidClientErrNotConnected,
		}
	}

	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-watchDone:
		}
	}()

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("iossimdriver: generate websocket key: %w", err)
	}
	handshake := fmt.Sprintf(
		"GET / HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		c.port, base64.StdEncoding.EncodeToString(key),
	)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		_ = conn.Close()
		return nil, nil, &HidClientError{
			Message: fmt.Sprintf("hid-client: WS connect failed: %v", err),
			Code:    HidClientErrNotConnected,
		}
	}

	header, leftover, err := readHandshakeResponse(conn)
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &HidClientError{
			Message: fmt.Sprintf("hid-client: WS connect failed: %v", err),
			Code:    HidClientErrNotConnected,
		}
	}
	if !strings.HasPrefix(header, "HTTP/1.1 101") {
		_ = conn.Close()
		statusLine := strings.SplitN(header, "\r\n", 2)[0]
		return nil, nil, &HidClientError{
			Message: fmt.Sprintf("hid-client: WS upgrade rejected: %s", statusLine),
			Code:    HidClientErrNotConnected,
		}
	}
	return conn, leftover, nil
}

// readHandshakeResponse reads from conn until a full HTTP header
// block ("\r\n\r\n") has arrived, returning the header text and any
// bytes read past it, which is the start of the server's first WS frames.
func readHandshakeResponse(conn net.Conn) (string, []byte, error) {
	var buf []byte
	tmp := make([]byte, 512)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if idx := bytes.Index(buf, []byte("\r\n\r\n")); idx >= 0 {
				return string(buf[:idx]), append([]byte(nil), buf[idx+4:]...), nil
			}
		}
		if err != nil {
			return "", nil, err
		}
	}
}

// readLoop accumulates raw bytes from conn, extracts complete WS
// frames, and hands each text frame's payload to handleLine. It runs
// until conn.Read fails, at which point it reports the connection
// lost to every pending dispatch.
func (c *HidClient) readLoop(conn net.Conn, seed []byte) {
	buf := append([]byte(nil), seed...)
	drain := func() {
		for {
			opcode, payload, ok := extractWsFrame(&buf)
			if !ok {
				return
			}
			if opcode == wsOpcodeText {
				c.handleLine(strings.TrimSpace(string(payload)))
			}
		}
	}
	drain()

	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			drain()
		}
		if err != nil {
			c.handleDisconnect(fmt.Sprintf("socket error: %v", err))
			return
		}
	}
}

type replyPayload struct {
	Ok    *bool  `json:"ok"`
	ID    *int   `json:"id"`
	Error string `json:"error"`
}

// handleLine decodes one reply line and resolves the pending dispatch
// it correlates to via ID. A line with no registered pending entry
// (an unknown ID, or a reply that arrived after a timeout already
// removed the entry) is silently dropped, matching a reply the caller
// no longer waits for.
func (c *HidClient) handleLine(line string) {
	if line == "" {
		return
	}
	var msg replyPayload
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	if msg.ID == nil {
		return
	}

	c.mu.Lock()
	pending, found := c.pending[*msg.ID]
	if found {
		delete(c.pending, *msg.ID)
	}
	c.mu.Unlock()
	if !found {
		return
	}

	if msg.Ok != nil && *msg.Ok {
		pending.replyCh <- nil
		return
	}
	errMsg := msg.Error
	if errMsg == "" {
		errMsg = fmt.Sprintf("hid-client: dispatch id=%d returned ok:false", *msg.ID)
	}
	pending.replyCh <- &HidClientError{Message: errMsg, Code: HidClientErrReplyError}
}

// handleDisconnect fails every pending dispatch with a disconnected
// error and marks the client dead. It is idempotent: a second call
// after the client is already dead does nothing, so a read error
// racing a concurrent Dispose never double-reports.
func (c *HidClient) handleDisconnect(reason string) {
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return
	}
	c.dead = true
	c.conn = nil
	pending := c.pending
	c.pending = map[int]pendingReply{}
	c.mu.Unlock()

	err := &HidClientError{Message: fmt.Sprintf("hid-client disconnected: %s", reason), Code: HidClientErrDisconnected}
	for _, p := range pending {
		p.replyCh <- err
	}
}

// Dispose closes the connection and fails every pending dispatch. It
// is safe to call multiple times and safe to call before Connect.
func (c *HidClient) Dispose() error {
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return nil
	}
	c.dead = true
	conn := c.conn
	c.conn = nil
	pending := c.pending
	c.pending = map[int]pendingReply{}
	c.mu.Unlock()

	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	err := &HidClientError{Message: "client disposed", Code: HidClientErrDisconnected}
	for _, p := range pending {
		p.replyCh <- err
	}
	return closeErr
}

// dispatch sends payload as a WS text frame with a fresh correlation
// id and waits for the matching reply, a timeout, or ctx
// cancellation, whichever comes first. The "id" key in payload, if
// any, is overwritten by the dispatch's own correlation id: the
// helper's wire protocol carries exactly one id per message, used to
// match a reply to its request, not to distinguish concurrent touch
// contacts. The legacy HID digitizer this client drives only ever
// tracks one active contact at a time.
func (c *HidClient) dispatch(ctx context.Context, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	if c.conn == nil || c.dead {
		c.mu.Unlock()
		return &HidClientError{Message: "hid-client: not connected, call Connect first", Code: HidClientErrNotConnected}
	}
	id := c.nextID
	c.nextID++
	replyCh := make(chan error, 1)
	c.pending[id] = pendingReply{replyCh: replyCh}
	conn := c.conn
	c.mu.Unlock()

	payload["id"] = id
	line, err := json.Marshal(payload)
	if err != nil {
		c.removePending(id)
		return fmt.Errorf("iossimdriver: encode hid dispatch: %w", err)
	}
	frame, err := encodeWsTextFrame(append(line, '\n'))
	if err != nil {
		c.removePending(id)
		return err
	}
	if _, err := conn.Write(frame); err != nil {
		c.removePending(id)
		return err
	}

	timer := time.NewTimer(c.dispatchTimeout)
	defer timer.Stop()
	select {
	case err := <-replyCh:
		return err
	case <-timer.C:
		c.removePending(id)
		return classifyDispatchTimeout(&HidClientError{
			Message: fmt.Sprintf("hid-client: dispatch id=%d timed out after %dms", id, c.dispatchTimeout.Milliseconds()),
			Code:    HidClientErrTimeout,
		})
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	}
}

func (c *HidClient) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Tap dispatches a tap-and-hold-then-release gesture at p, held for
// holdMs.
func (c *HidClient) Tap(ctx context.Context, p NormalizedPoint, holdMs int) error {
	return c.dispatch(ctx, map[string]any{"type": "tap", "x": p.X, "y": p.Y, "holdMs": holdMs})
}

// Swipe dispatches a linear drag from "from" to "to" over durationMs,
// converted to the helper's (steps, stepMs) shape.
func (c *HidClient) Swipe(ctx context.Context, from, to NormalizedPoint, durationMs int) error {
	steps := roundSwipeSteps(durationMs)
	return c.dispatch(ctx, map[string]any{
		"type":    "swipe",
		"x1":      from.X,
		"y1":      from.Y,
		"x2":      to.X,
		"y2":      to.Y,
		"steps":   steps,
		"stepMs":  swipeStepMs,
		"dwellMs": 0,
	})
}

// roundSwipeSteps maps durationMs to a step count spaced swipeStepMs
// apart, at least one step and at most maxSwipeSteps.
func roundSwipeSteps(durationMs int) int {
	steps := int(math.Round(float64(durationMs) / float64(swipeStepMs)))
	if steps < 1 {
		steps = 1
	}
	if steps > maxSwipeSteps {
		steps = maxSwipeSteps
	}
	return steps
}

// TouchDown dispatches the first phase of a streaming touch contact
// at p.
func (c *HidClient) TouchDown(ctx context.Context, p NormalizedPoint) error {
	return c.dispatch(ctx, map[string]any{"type": "touch-down", "x": p.X, "y": p.Y})
}

// TouchMove dispatches a move phase of an in-progress streaming touch
// contact to p.
func (c *HidClient) TouchMove(ctx context.Context, p NormalizedPoint) error {
	return c.dispatch(ctx, map[string]any{"type": "touch-move", "x": p.X, "y": p.Y})
}

// TouchUp dispatches the final phase of a streaming touch contact at
// p.
func (c *HidClient) TouchUp(ctx context.Context, p NormalizedPoint) error {
	return c.dispatch(ctx, map[string]any{"type": "touch-up", "x": p.X, "y": p.Y})
}
