package orchestra

import "dev.atomyx/internal/diag"

// EmptySelectorError is returned when a caller resolves, waits for, or
// acts on a selector that sets no field. It signals a malformed
// request, most often a selector nested under the wrong key, rather
// than an element that is missing from the screen.
type EmptySelectorError struct{}

func (e *EmptySelectorError) Error() string {
	return "selector sets no field, so it would match the whole screen"
}

// Code implements diag.Coded.
func (e *EmptySelectorError) Code() string { return diag.CodeInvalidArgument }

// Remedy implements diag.Coded.
func (e *EmptySelectorError) Remedy() string {
	return "Set at least one of id, text, label, hint, value, role, enabled, clickable or focused " +
		"at the level the tool's description shows. The nth field alone does not select " +
		"an element, and keys a selector does not take are ignored, so a misspelled or " +
		"misplaced key leaves the selector empty."
}

var _ diag.Coded = (*EmptySelectorError)(nil)

// RequireNonEmpty returns an *EmptySelectorError when s places no constraint
// on any element, and nil otherwise.
func RequireNonEmpty(s Selector) error {
	if s.IsEmpty() {
		return &EmptySelectorError{}
	}
	return nil
}
