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
// keeps that code.
const (
	codeNoTree           = "NO_TREE"
	codeNoScreenshot     = "NO_SCREENSHOT"
	codeSessionClosed    = "SESSION_CLOSED"
	codeDeviceReleased   = "DEVICE_RELEASED"
	codeTreeStale        = "TREE_STALE"
	codeNodeNotFound     = "NODE_NOT_FOUND"
	codeHostNotAllowed   = "HOST_NOT_ALLOWED"
	codeOriginNotAllowed = "ORIGIN_NOT_ALLOWED"
	codeTokenRequired    = "TOKEN_REQUIRED"
	codeCodeRefused      = "CODE_REFUSED"
	codeInternal         = "INTERNAL"
)

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
// other failure is reported as an internal error without its text,
// because the text of an unclassified error may name host details.
func writeFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrClosed):
		writeCoded(w, http.StatusGone, codeSessionClosed, "the session is closed", "Reconnect to the device to inspect it again.")
		return
	case errors.Is(err, ErrReleased):
		writeCoded(w, http.StatusGone, codeDeviceReleased, "the inspector released the device after it sat idle",
			"Press reconnect on the page to take the device again.")
		return
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeCoded(w, http.StatusServiceUnavailable, codeInternal, "the request was cancelled before it finished", "")
		return
	}
	code, message, remedy := diag.Describe(err)
	switch code {
	case diag.CodeHolderBusy, diag.CodeDeviceHeld:
		writeCoded(w, http.StatusConflict, code, message, remedy)
	case diag.CodeInvalidArgument:
		writeCoded(w, http.StatusBadRequest, code, message, remedy)
	case diag.CodeUnknown:
		writeCoded(w, http.StatusInternalServerError, codeInternal, "the request failed", "")
	default:
		writeCoded(w, http.StatusBadGateway, code, message, remedy)
	}
}
