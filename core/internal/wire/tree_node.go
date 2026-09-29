package wire

// TreeNodeWire is the normalized, cross-platform UI tree node shape
// a driver layer produces from a raw Android or iOS agent tree. It
// is not decoded directly off either agent's wire: each agent's raw
// tree shape lives with its own transport client, and normalizing
// one into this shape is a later stage than the transport layer.
//
// Shape invariants a decoder relies on:
//
//   - Attributes is a flat string-to-string map. Keys outside the
//     canonical set (id, text, label, hint, value, role, class,
//     package, bounds) are non-portable extension data and are
//     expected to start with "ext:".
//   - Children is an ordered list; later siblings render on top of
//     earlier ones for z-order detection.
//   - The state fields (Clickable, Enabled, Focused, Selected, Checked,
//     Visible) are tri-state: a nil pointer means the underlying
//     accessibility runtime did not report the value, which is a
//     distinct decoded state from an explicit true or false. Callers
//     must not treat nil as false.
type TreeNodeWire struct {
	Attributes map[string]string `json:"attributes"`
	Children   []TreeNodeWire    `json:"children"`
	Clickable  *bool             `json:"clickable,omitempty"`
	Enabled    *bool             `json:"enabled,omitempty"`
	Focused    *bool             `json:"focused,omitempty"`
	Selected   *bool             `json:"selected,omitempty"`
	Checked    *bool             `json:"checked,omitempty"`
	Visible    *bool             `json:"visible,omitempty"`
}
