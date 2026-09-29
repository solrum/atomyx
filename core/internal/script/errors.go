package script

import "fmt"

// ParseError reports a malformed script that failed before or during
// validation: an unrecognized step command, a shorthand value of the
// wrong shape, or a validation-rule violation. Its Error() text is
// informational only. Callers must not match on its wording, only
// on whether ParseScript returned a non-nil error.
type ParseError struct {
	Message string
}

func (e *ParseError) Error() string { return e.Message }

func newParseError(format string, args ...any) *ParseError {
	return &ParseError{Message: fmt.Sprintf(format, args...)}
}
