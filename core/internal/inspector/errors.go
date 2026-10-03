package inspector

import (
	"context"
	"errors"
	"net/http"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// Codes the inspector reports for conditions that are specific to it.
// Every code in an error body is upper snake case, the same convention
// as the diag codes, and a failure that already carries a diag code
// keeps that code. A failure with no code is reported as the diag
// code UNKNOWN.
const (
	codeNoTree           = "NO_TREE"
	codeNoScreenshot     = "NO_SCREENSHOT"
	codeSessionClosed    = "SESSION_CLOSED"
	codeDeviceReleased   = "DEVICE_RELEASED"
	codeDeviceLost       = "DEVICE_LOST"
	codeHolderExited     = "HOLDER_EXITED"
	codeTreeStale        = "TREE_STALE"
	codeNodeNotFound     = "NODE_NOT_FOUND"
	codeHostNotAllowed   = "HOST_NOT_ALLOWED"
	codeOriginNotAllowed = "ORIGIN_NOT_ALLOWED"
	codeTokenRequired    = "TOKEN_REQUIRED"
	codeCodeRefused      = "CODE_REFUSED"
)

// statusClientClosedRequest is the status for a request whose caller
// went away before the answer was ready. net/http has no name for it.
const statusClientClosedRequest = 499

// writeCoded writes the error payload every failing /api/ response
// carries: a stable code, a message, and a remedy when one is known.
func writeCoded(w http.ResponseWriter, status int, code, message, remedy string) {
	body := map[string]string{"code": code, "message": message}
	if remedy != "" {
		body["remedy"] = remedy
	}
	writeJSON(w, status, map[string]map[string]string{"error": body})
}

// writeFailure reports err with the HTTP status its code calls for.
// A diag-coded failure keeps its code, message and remedy, and any
// other failure is reported as UNKNOWN with status 500 and without its
// text, because the text of an unclassified error may name host
// details. A request whose caller canceled it is reported as UNKNOWN
// with status 499, the way a canceled MCP tool call carries no code of
// its own, and a request that ran out of time is reported as
// TOOL_TIMEOUT with status 504, the code an MCP tool call gets when
// its budget elapses.
func writeFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrClosed):
		writeCoded(w, http.StatusGone, codeSessionClosed, "the session is closed", "Reconnect to the device to inspect it again.")
		return
	case errors.Is(err, ErrBorrowed):
		writeCoded(w, http.StatusGone, codeDeviceReleased, "the inspector handed the device to another process",
			"Press reconnect on the page to watch that process or to take the device again.")
		return
	case errors.Is(err, ErrReleased):
		writeCoded(w, http.StatusGone, codeDeviceReleased, "the inspector released the device after it sat idle",
			"Press reconnect on the page to take the device again.")
		return
	case errors.Is(err, ErrHolderExited):
		writeCoded(w, http.StatusGone, codeHolderExited, "the process that held the device has exited",
			"Press reconnect on the page to take the device, or to watch its next holder.")
		return
	case errors.Is(err, ErrLost):
		writeCoded(w, http.StatusGone, codeDeviceLost, "the device was lost",
			"Check that the device is connected and awake, then press reconnect on the page.")
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeCoded(w, http.StatusGatewayTimeout, diag.CodeToolTimeout, "the request ran out of time before it finished",
			"Try again. If it keeps timing out, check that the device responds, or press reconnect.")
		return
	case errors.Is(err, context.Canceled):
		writeCoded(w, statusClientClosedRequest, diag.CodeUnknown, "the request was cancelled before it finished", "")
		return
	}
	code, message, remedy := diag.Describe(err)
	switch code {
	case diag.CodeHolderBusy, diag.CodeDeviceHeld:
		writeCoded(w, http.StatusConflict, code, message, remedy)
	case diag.CodeInvalidArgument:
		writeCoded(w, http.StatusBadRequest, code, message, remedy)
	case diag.CodeUnknown:
		writeCoded(w, http.StatusInternalServerError, diag.CodeUnknown, "the request failed", "")
	default:
		writeCoded(w, http.StatusBadGateway, code, message, remedy)
	}
}
