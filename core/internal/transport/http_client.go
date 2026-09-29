package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dev.atomyx/internal/logging"
)

// defaultHTTPClientTimeout bounds a single request-response round
// trip when the caller has not supplied its own context deadline. A
// hung Android agent connection (the process alive but the control
// server wedged) would otherwise block the calling goroutine
// indefinitely, since http.Client{} with a zero Timeout has no
// backstop of its own. A per-request context deadline set by the
// caller still applies on top of this and can be tighter or looser;
// this constant only bounds requests that arrive with no deadline of
// their own.
const defaultHTTPClientTimeout = 10 * time.Second

// HTTPClient issues requests against the Android agent's HTTP
// control server (Kotlin HttpControlServer, default port 8765) and
// decodes its responses into the shapes declared in
// android_types.go. One method exists per route the agent serves.
type HTTPClient struct {
	baseURL string
	client  *http.Client
	logger  logging.Logger
}

// HTTPClientOption configures an HTTPClient constructed by NewHTTPClient.
type HTTPClientOption func(*HTTPClient)

// WithHTTPClient replaces the underlying *http.Client, for example to
// install a transport with custom TLS or proxy settings, or to
// override defaultHTTPClientTimeout with a caller-chosen Timeout.
func WithHTTPClient(c *http.Client) HTTPClientOption {
	return func(h *HTTPClient) {
		h.client = c
	}
}

// WithHTTPLogger replaces the logger every request logs a completion
// line through. The default is logging.NoopLogger{}, so a caller that
// never calls WithHTTPLogger keeps building a client that logs
// nothing.
func WithHTTPLogger(l logging.Logger) HTTPClientOption {
	return func(h *HTTPClient) {
		h.logger = l
	}
}

// NewHTTPClient builds a client bound to the Android agent listening
// at host:port. Every request-issuing method takes a context.Context;
// canceling it aborts the in-flight request. The client carries
// defaultHTTPClientTimeout as a backstop against a hung connection
// unless WithHTTPClient supplies a client with its own Timeout.
func NewHTTPClient(host string, port int, opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		client:  &http.Client{Timeout: defaultHTTPClientTimeout},
		logger:  logging.NoopLogger{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// HTTPError reports an Android agent HTTP request that failed either
// at the transport level (Err set) or by returning a non-2xx status
// (StatusCode and Body set).
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
	Err        error

	// transportLevel marks Err as coming from the dial, write, or read
	// stage of the request rather than from encoding the request body
	// or decoding the response: classifyHTTPError only turns an
	// HTTPError into an UnreachableError when this is true, since an
	// encode or decode failure means the agent was reached, not that
	// it was unreachable.
	transportLevel bool
}

func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("transport: %s %s: %v", e.Method, e.Path, e.Err)
	}
	return fmt.Sprintf("transport: %s %s: unexpected status %d: %s",
		e.Method, e.Path, e.StatusCode, e.Body)
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

// do issues an HTTP request against path, encoding body as the JSON
// request payload when non-nil, and decoding the response into out
// when out is non-nil. A canceled or expired ctx surfaces as
// ctx.Err() rather than the underlying transport error, so callers
// can match on it directly. Every other failure is classified by
// classifyHTTPError: a request that runs past the client's own
// defaultHTTPClientTimeout fallback, because ctx carried no deadline
// of its own, is reported as a diag.CodeToolTimeout diagnostic; a
// dial, write, or read failure is reported as an *UnreachableError; a
// non-2xx response is reported as an *AgentError; anything else keeps
// its existing classification (or lack of one). Every call logs one
// debug-level completion line through logWireCall, whichever way it
// ends.
func (c *HTTPClient) do(ctx context.Context, method, path string, body any, out any) error {
	start := time.Now()
	err := c.doRequest(ctx, method, path, body, out)
	err = classifyHTTPError(err, method, path)
	c.logWireCall(ctx, path, err, time.Since(start))
	return err
}

// logWireCall emits one minimal structured debug line for a completed
// wire call: the route, whether it succeeded, and how long it took.
// It never logs the request or response payload. The run id is
// attached when ctx carries one, so a driver-layer failure can be
// tied back to the run that produced it.
func (c *HTTPClient) logWireCall(ctx context.Context, path string, err error, duration time.Duration) {
	logging.ChildFromContext(c.logger, ctx).Debug("driver.wireCall", logging.LogContext{
		"command":    path,
		"ok":         err == nil,
		"durationMs": duration.Milliseconds(),
	})
}

// doRequest is the wire call do wraps: it performs the request and
// decodes the response, with no logging of its own.
func (c *HTTPClient) doRequest(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return &HTTPError{Method: method, Path: path, Err: fmt.Errorf("encode request: %w", err)}
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return &HTTPError{Method: method, Path: path, Err: err}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &HTTPError{Method: method, Path: path, Err: err, transportLevel: true}
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &HTTPError{Method: method, Path: path, Err: fmt.Errorf("read response: %w", err), transportLevel: true}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       string(data),
		}
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &HTTPError{Method: method, Path: path, Err: fmt.Errorf("decode response: %w", err)}
	}
	return nil
}

// Health issues GET /health. It answers even when the agent's
// AccessibilityService is not yet connected.
func (c *HTTPClient) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	err := c.do(ctx, http.MethodGet, pathHealth, nil, &out)
	return out, err
}

// Ping issues GET /ping, the capability handshake a caller runs once
// after Health succeeds.
func (c *HTTPClient) Ping(ctx context.Context) (PingResponse, error) {
	var out PingResponse
	err := c.do(ctx, http.MethodGet, pathPing, nil, &out)
	return out, err
}

// Tree issues GET /tree and returns the raw agent accessibility
// tree. Normalizing it into the canonical wire.TreeNodeWire shape is
// a later stage than this client.
func (c *HTTPClient) Tree(ctx context.Context) (AndroidTreeNode, error) {
	var out AndroidTreeNode
	err := c.do(ctx, http.MethodGet, pathTree, nil, &out)
	return out, err
}

// Keyboard issues GET /keyboard.
func (c *HTTPClient) Keyboard(ctx context.Context) (KeyboardResponse, error) {
	var out KeyboardResponse
	err := c.do(ctx, http.MethodGet, pathKeyboard, nil, &out)
	return out, err
}

// Screenshot issues GET /screenshot.
func (c *HTTPClient) Screenshot(ctx context.Context) (ScreenshotResponse, error) {
	var out ScreenshotResponse
	err := c.do(ctx, http.MethodGet, pathScreenshot, nil, &out)
	return out, err
}

// CurrentActivity issues GET /current-activity.
func (c *HTTPClient) CurrentActivity(ctx context.Context) (CurrentActivityResponse, error) {
	var out CurrentActivityResponse
	err := c.do(ctx, http.MethodGet, pathCurrentActivity, nil, &out)
	return out, err
}

// Apps issues GET /apps.
func (c *HTTPClient) Apps(ctx context.Context) ([]InstalledApp, error) {
	var out []InstalledApp
	err := c.do(ctx, http.MethodGet, pathApps, nil, &out)
	return out, err
}

// TapCoords issues POST /actions/tap_coords.
func (c *HTTPClient) TapCoords(ctx context.Context, req TapCoordsRequest) (GestureResult, error) {
	var out GestureResult
	err := c.do(ctx, http.MethodPost, pathTapCoords, req, &out)
	return out, err
}

// LongPress issues POST /actions/long_press.
func (c *HTTPClient) LongPress(ctx context.Context, req LongPressRequest) (GestureResult, error) {
	var out GestureResult
	err := c.do(ctx, http.MethodPost, pathLongPress, req, &out)
	return out, err
}

// ClearFocusedInput issues POST /actions/clear_focused_input. The
// agent accepts no request body for this route.
func (c *HTTPClient) ClearFocusedInput(ctx context.Context) (ClearFocusedInputResult, error) {
	var out ClearFocusedInputResult
	err := c.do(ctx, http.MethodPost, pathClearFocusedInput, struct{}{}, &out)
	return out, err
}

// HideKeyboard issues POST /actions/hide_keyboard. The agent accepts
// no request body for this route.
func (c *HTTPClient) HideKeyboard(ctx context.Context) (AckResult, error) {
	var out AckResult
	err := c.do(ctx, http.MethodPost, pathHideKeyboard, struct{}{}, &out)
	return out, err
}

// DispatchGesture issues POST /actions/dispatch_gesture.
func (c *HTTPClient) DispatchGesture(ctx context.Context, req DispatchGestureRequest) (DispatchGestureResult, error) {
	var out DispatchGestureResult
	err := c.do(ctx, http.MethodPost, pathDispatchGesture, req, &out)
	return out, err
}

// Swipe issues POST /actions/swipe.
func (c *HTTPClient) Swipe(ctx context.Context, req SwipeRequest) (GestureResult, error) {
	var out GestureResult
	err := c.do(ctx, http.MethodPost, pathSwipe, req, &out)
	return out, err
}

// TypeKeyboard issues POST /actions/type_keyboard.
func (c *HTTPClient) TypeKeyboard(ctx context.Context, req TypeKeyboardRequest) (TypeKeyboardResult, error) {
	var out TypeKeyboardResult
	err := c.do(ctx, http.MethodPost, pathTypeKeyboard, req, &out)
	return out, err
}

// PressKey issues POST /actions/key.
func (c *HTTPClient) PressKey(ctx context.Context, req KeyRequest) (OkResponse, error) {
	var out OkResponse
	err := c.do(ctx, http.MethodPost, pathKey, req, &out)
	return out, err
}

// LaunchApp issues POST /actions/launch.
func (c *HTTPClient) LaunchApp(ctx context.Context, req LaunchRequest) (OkResponse, error) {
	var out OkResponse
	err := c.do(ctx, http.MethodPost, pathLaunch, req, &out)
	return out, err
}

// ForceStopApp issues POST /actions/force_stop. Ok reports whether the
// agent actually stopped the package; Reason explains a refusal (for
// example, the agent lacking the platform permission to stop another
// app's process).
func (c *HTTPClient) ForceStopApp(ctx context.Context, req ForceStopRequest) (AckResult, error) {
	var out AckResult
	err := c.do(ctx, http.MethodPost, pathForceStop, req, &out)
	return out, err
}
