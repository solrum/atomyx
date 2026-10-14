package setup

import "dev.atomyx/internal/diag"

// failedFrom builds the Result of a step that tried something and got
// err back. The message names what was attempted and carries the
// error's own user-facing text. The remedy is the error's remedy when
// it has one and fallback otherwise.
func failedFrom(attempt string, err error, fallback string) Result {
	_, message, remedy := diag.Describe(err)
	if remedy == "" {
		remedy = fallback
	}
	return Result{Status: StatusFailed, Message: attempt + ": " + message, Remedy: remedy}
}
