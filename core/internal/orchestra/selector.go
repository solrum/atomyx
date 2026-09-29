package orchestra

// Selector is the platform-neutral element query shape: it
// describes WHAT a caller is looking for, not HOW to find it.
// Compiling it into an ElementFilter with priority-broadening
// policy is CompileSelector's job (see priority_broadening.go).
//
// All fields are optional. At least one content-matching field
// (ID, Text, Label, Hint, Value) should be provided, otherwise the
// selector matches every element in the tree.
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
