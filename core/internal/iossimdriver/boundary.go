package iossimdriver

import (
	"errors"

	"dev.atomyx/internal/diag"
)

// failHid is the boundary every IosSimDriver method that dispatches a
// touch event over the HID connection routes its returned error
// through before it reaches a caller. It delegates to
// diag.ClassifyAgentBoundary, recognizing an unreachable agent through
// HidClientErrNotConnected and HidClientErrDisconnected and a rejected
// request through HidClientErrReplyError, so a tool-facing consumer
// never sees the HID helper's WS address or dispatch id: only Error()
// (used for logs, never surfaced to a caller) carries that detail.
// HidClientErrReplyError carries only the helper's error string, no
// code or category distinguishing a rejection caused by the caller's
// own arguments from any other kind, so this boundary never reports
// diag.CodeInvalidArgument.
func (d *IosSimDriver) failHid(err error) error {
	return diag.ClassifyAgentBoundary(err, d.udid,
		func(err error) bool {
			var hidErr *HidClientError
			if errors.As(err, &hidErr) {
				return hidErr.Code == HidClientErrNotConnected || hidErr.Code == HidClientErrDisconnected
			}
			return false
		},
		func(error) (string, bool) { return "", false },
		func(err error) (string, bool) {
			var hidErr *HidClientError
			if errors.As(err, &hidErr) && hidErr.Code == HidClientErrReplyError {
				return hidErr.Message, true
			}
			return "", false
		})
}
