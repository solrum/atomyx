package runner

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/orchestra"
)

// pollFailure converts the error of a polling wait into a failed
// CommandResult. A deadline that passed without a match reports
// timeoutDetail, followed by the last hierarchy read error when the
// final poll could not read the screen. Any other error fails the step
// with its own message and diag code, so the report names the real
// cause instead of an internal exception.
//
// A context error is re-raised because it means the caller stopped the
// run, which the runner reports as an interruption.
func pollFailure(err error, timeoutDetail string) CommandResult {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		panic(err)
	}
	var timeoutErr *orchestra.FindTimeoutError
	if errors.As(err, &timeoutErr) {
		detail := timeoutDetail
		if timeoutErr.LastReadErr != nil {
			detail = fmt.Sprintf("%s (last screen read failed: %v)", timeoutDetail, timeoutErr.LastReadErr)
		}
		return CommandResult{Ok: false, Detail: strPtr(detail), Code: failureCode(err)}
	}
	return CommandResult{Ok: false, Detail: strPtr(err.Error()), Code: failureCode(err)}
}

func failureCode(err error) string {
	if code, _, _ := diag.Describe(err); code != diag.CodeUnknown {
		return code
	}
	return ""
}
