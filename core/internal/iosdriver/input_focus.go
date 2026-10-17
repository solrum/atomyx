package iosdriver

import (
	"errors"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/transport"
)

// keyboardFocusLostMarker is the phrase XCTest uses when it cannot
// synthesize keys because no element holds keyboard focus ("Neither
// element nor any descendant has keyboard focus"). The runner relays
// the framework message unchanged.
const keyboardFocusLostMarker = "keyboard focus"

const (
	inputNotFocusedMessage = "nothing was typed because no input field has keyboard focus"
	inputNotFocusedRemedy  = "Tap the input field so it has focus, then type again."
)

func newInputNotFocused(cause error) error {
	return diag.New(diag.CodeInputNotFocused, inputNotFocusedMessage, inputNotFocusedRemedy, cause)
}

// failInput routes the error of a keyboard-driven call (type or erase)
// through the driver boundary, first recognizing a runner rejection
// that reports missing keyboard focus and reporting it as
// INPUT_NOT_FOCUSED so a caller can re-focus and retry.
func (d *IosDriver) failInput(err error) error {
	var agentErr *transport.AgentError
	if errors.As(err, &agentErr) && strings.Contains(agentErr.Message, keyboardFocusLostMarker) {
		return newInputNotFocused(err)
	}
	return d.fail(err)
}
