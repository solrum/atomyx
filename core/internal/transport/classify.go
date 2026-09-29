package transport

import (
	"encoding/json"
	"errors"
	"fmt"
)

// UnreachableError reports that the on-device agent could not be
// reached at all: the dial failed, or an established connection was
// lost, reset, or closed before a response arrived. Error() names the
// route or command that failed, for logs; a caller reporting this
// failure to an end user should detect it with IsUnreachable and
// render its own neutral message rather than surfacing Error()'s
// text, which does name the route.
type UnreachableError struct {
	// Route identifies the wire call that failed: an HTTP "METHOD
	// /path" pair, or a TCP command type. Kept for logs only.
	Route string
	// Err is the underlying dial or connection failure.
	Err error
}

func (e *UnreachableError) Error() string {
	if e.Route == "" {
		return fmt.Sprintf("transport: agent unreachable: %v", e.Err)
	}
	return fmt.Sprintf("transport: agent unreachable calling %s: %v", e.Route, e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// IsUnreachable reports whether err is, or wraps, an UnreachableError.
func IsUnreachable(err error) bool {
	var unreachable *UnreachableError
	return errors.As(err, &unreachable)
}

// AgentError reports that the agent answered a request but reported
// failure itself, as opposed to the request never reaching it or the
// connection breaking in flight. Message is the agent's own failure
// text: safe to surface to a caller because it names no transport
// route, host, or port. Route and StatusCode are retained for logs
// only; StatusCode is the HTTP status the failure carried and is zero
// for an AgentError sourced from the iOS agent's TCP protocol, which
// has no status code of its own.
type AgentError struct {
	Route      string
	StatusCode int
	Message    string
	Err        error
}

func (e *AgentError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("transport: agent rejected %s with status %d: %s", e.Route, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("transport: agent rejected %s: %s", e.Route, e.Message)
}

func (e *AgentError) Unwrap() error { return e.Err }

// classifyHTTPError turns the *HTTPError doRequest can produce into
// the typed shape a caller above this package classifies on: a
// fallback-timeout wrap (unchanged from before this classification
// existed), an UnreachableError for a failure at the dial, write, or
// read level, an AgentError for a non-2xx status the agent itself
// returned, or the original error unchanged for anything else (a
// local encode or decode failure, or a caller's own context error,
// which doRequest already returns unwrapped).
func classifyHTTPError(err error, method, path string) error {
	if err == nil {
		return nil
	}
	if isHTTPFallbackTimeout(err) {
		return asToolTimeout(err, true,
			fmt.Sprintf("the agent did not answer %s %s within its wire-call time budget", method, path))
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return err
	}
	route := fmt.Sprintf("%s %s", method, path)
	if httpErr.Err != nil {
		if !httpErr.transportLevel {
			return httpErr
		}
		return &UnreachableError{Route: route, Err: httpErr}
	}
	return &AgentError{
		Route:      route,
		StatusCode: httpErr.StatusCode,
		Message:    agentErrorMessage(httpErr.Body, httpErr.StatusCode),
		Err:        httpErr,
	}
}

// agentBodyFields is the JSON shape the Android agent's RouteResponse
// failure constructors produce: {"error": "..."}. message is accepted
// as an alternate field name, in case a route ever reports through it
// instead of error. reason is a third alternate: the /health route
// reports a not-ready outcome as {"ok":false,...,"reason":"..."} with
// neither error nor message set, since the response describes a
// liveness state rather than a rejected request.
type agentBodyFields struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// agentErrorMessage extracts the agent's own failure text from body,
// the raw HTTP response body of a non-2xx status. The full body stays
// reachable through AgentError's Err chain for logs regardless of what
// this returns, so falling back here never loses that detail.
//
// body is expected to be JSON with an "error", "message", or "reason"
// string field, checked in that order. Anything else, including an
// empty body, a body that fails to parse as JSON, or JSON with none of
// the three fields populated, falls back to a neutral message naming
// only the status code, so a malformed or unexpected body never leaks
// its raw bytes into a message a caller may surface.
func agentErrorMessage(body string, statusCode int) string {
	var fields agentBodyFields
	if err := json.Unmarshal([]byte(body), &fields); err == nil {
		if fields.Error != "" {
			return fields.Error
		}
		if fields.Message != "" {
			return fields.Message
		}
		if fields.Reason != "" {
			return fields.Reason
		}
	}
	return fmt.Sprintf("the agent reported status %d with no further detail", statusCode)
}

// classifyTCPError turns the *TCPError callInner can produce into the
// same typed shape classifyHTTPError produces for the HTTP client: a
// fallback-timeout wrap (unchanged), an UnreachableError for a
// connection that was never established or was lost, an AgentError
// for the agent answering ok:false, or the original error unchanged
// for anything else (a caller's own context error, which callInner
// already returns unwrapped).
func classifyTCPError(err error, cmdType string) error {
	if err == nil {
		return nil
	}
	if isTCPFallbackTimeout(err) {
		return asToolTimeout(err, true,
			fmt.Sprintf("the agent did not answer %q within its wire-call time budget", cmdType))
	}
	var tcpErr *TCPError
	if !errors.As(err, &tcpErr) {
		return err
	}
	switch tcpErr.Code {
	case TCPErrNotConnected, TCPErrDisconnected:
		return &UnreachableError{Route: cmdType, Err: tcpErr}
	case TCPErrDriverError:
		return &AgentError{Route: cmdType, Message: tcpErr.Message, Err: tcpErr}
	default:
		return tcpErr
	}
}
