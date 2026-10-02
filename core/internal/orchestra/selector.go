package orchestra

// Selector is the platform-neutral element query shape: it
// describes WHAT a caller is looking for, not HOW to find it.
// Compiling it into an ElementFilter with priority-broadening
// policy is CompileSelector's job (see priority_broadening.go).
//
// Every field is optional, but a selector must set at least one field
// (content, role, state, or nth). A selector that sets none is rejected
// with EmptySelectorError by Orchestra's public entry points. A role
// or state with no content field matches every element that satisfies
// it, in document order.
type Selector struct {
	// ID matches the canonical "id" attribute.
	ID *Pattern `json:"id,omitempty"`
	// Text matches the canonical "text" attribute.
	Text *Pattern `json:"text,omitempty"`
	// Label matches the canonical "label" (accessibility) attribute.
	Label *Pattern `json:"label,omitempty"`
	// Hint matches the canonical "hint" attribute.
	Hint *Pattern `json:"hint,omitempty"`
	// Value matches the canonical "value" attribute.
	Value *Pattern `json:"value,omitempty"`

	// Role constrains matches to a normalized semantic role (for
	// example "button").
	Role *string `json:"role,omitempty"`

	// Enabled, Clickable, and Focused constrain matches to that
	// state. A false or absent value places no constraint; only an
	// explicit true value is applied, matching CompileSelector's
	// broadening rules.
	Enabled   *bool `json:"enabled,omitempty"`
	Clickable *bool `json:"clickable,omitempty"`
	Focused   *bool `json:"focused,omitempty"`

	// Nth selects a specific zero-based index from the broadened
	// match list, in document order. Nil means "return every
	// broadened, ranked match".
	Nth *int `json:"nth,omitempty"`
}

// HasContent reports whether s carries at least one content-matching
// field. A selector with no content field matches every element in
// the tree, which callers may choose to reject, warn on, or accept
// depending on their own policy.
func (s Selector) HasContent() bool {
	return s.ID != nil || s.Text != nil || s.Label != nil || s.Hint != nil || s.Value != nil
}

// IsEmpty reports whether s places no constraint on any element: no
// content field, no role, no true state flag, and no position. Such a
// selector would match the whole tree, so the root node would resolve
// as if it were the caller's target.
func (s Selector) IsEmpty() bool {
	return !s.HasContent() && s.Role == nil && s.Nth == nil &&
		!isTrue(s.Enabled) && !isTrue(s.Clickable) && !isTrue(s.Focused)
}

func isTrue(b *bool) bool { return b != nil && *b }
