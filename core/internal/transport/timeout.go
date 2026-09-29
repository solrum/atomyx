package transport

import (
	"context"
	"errors"

	"dev.atomyx/internal/diag"
)

// wireCallTimeoutRemedy is the remedy text attached whenever a wire
// call's own fallback timeout fires, shared by every transport in
// this package so a caller sees the same guidance regardless of
// which transport issued the call.
const wireCallTimeoutRemedy = "Confirm the device and its agent are still reachable, then retry."

// asToolTimeout wraps err with diag.CodeToolTimeout, describing the
// failure as message, when isTimeout is true. It returns err
// unchanged otherwise, so every other failure a transport can produce
// keeps its own existing classification (or lack of one) instead of
// being folded into this one code.
func asToolTimeout(err error, isTimeout bool, message string) error {
	if err == nil || !isTimeout {
		return err
	}
	return diag.New(diag.CodeToolTimeout, message, wireCallTimeoutRemedy, err)
}

// isTCPFallbackTimeout reports whether err is a *TCPError carrying one
// of the codes TCPClient's own fallback bound assigns when a caller's
// context carries no deadline of its own: TCPErrWriteTimeout (a
// wedged peer that stopped draining its socket) or
// TCPErrResponseTimeout (an agent that accepted the request but never
// answered it). Every other TCPError code, including one produced
// while the caller's own context deadline was in effect, is not a
// fallback timeout and reports false.
func isTCPFallbackTimeout(err error) bool {
	var tcpErr *TCPError
	if !errors.As(err, &tcpErr) {
		return false
	}
	return tcpErr.Code == TCPErrWriteTimeout || tcpErr.Code == TCPErrResponseTimeout
}

// isHTTPFallbackTimeout reports whether err is an *HTTPError wrapping
// the failure http.Client.Timeout produces when it fires: the client
// applies that bound only in the absence of a caller-supplied context
// deadline, so a chain reaching context.DeadlineExceeded through an
// HTTPError here always means the agent did not answer within the
// client's own fallback bound, never the caller's own deadline (a
// caller deadline is checked, and returned as ctx.Err() directly
// rather than wrapped in an HTTPError, before this failure kind is
// ever constructed).
func isHTTPFallbackTimeout(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return errors.Is(httpErr.Err, context.DeadlineExceeded)
}
