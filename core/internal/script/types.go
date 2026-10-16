package script

import (
	"encoding/json"

	"dev.atomyx/internal/orchestra"
)

// ScriptSelector matches an element in the driver's accessibility
// tree. Every field is optional; leaving all of them unset is legal
// (priority broadening then has nothing to work with), so callers
// should carry at least one identifying field.
//
// Text, ID, Label and Hint match by exact string equality. Their
// Regex counterparts match by regular expression instead and are
// authored as a {regex, flags} object in place of the string. A field
// and its Regex counterpart are never both set. The JSON form carries
// either shape under the plain field name, see selector_json.go.
type ScriptSelector struct {
	Text       *string
	TextRegex  *RegexPattern
	ID         *string
	IDRegex    *RegexPattern
	Label      *string
	LabelRegex *RegexPattern
	Hint       *string
	HintRegex  *RegexPattern
	Role       *string
	Nth        *int
}

// RegexPattern is the authored form of a regular expression selector
// field: a JavaScript-syntax source and an optional flag string, the
// same {regex, flags} shape the MCP selector accepts. A match is an
// unanchored search, so anchor the source with ^ and $ to match a whole
// value.
type RegexPattern struct {
	Regex string `json:"regex"`
	Flags string `json:"flags,omitempty"`
}

// Compile builds the matcher for the pattern, applying the same flag
// set and unsupported-construct rejection the MCP selector applies.
func (p RegexPattern) Compile() (orchestra.Pattern, error) {
	return orchestra.NewRegexPattern(p.Regex, p.Flags)
}

// PointerAnchorValues lists the accepted PointerTarget anchor names.
var PointerAnchorValues = []string{
	"center", "topLeft", "topRight", "bottomLeft", "bottomRight",
	"top", "bottom", "left", "right",
}

func isValidPointerAnchor(v string) bool {
	for _, a := range PointerAnchorValues {
		if a == v {
			return true
		}
	}
	return false
}

// PinchAxisValues lists the accepted PinchStep axis names: the two
// contact points spread along the vertical axis or the horizontal one.
var PinchAxisValues = []string{"vertical", "horizontal"}

func isValidPinchAxis(v string) bool {
	for _, a := range PinchAxisValues {
		if a == v {
			return true
		}
	}
	return false
}

// Point is an absolute screen coordinate in points, the same unit as
// the bounds get_ui_tree reports and the center find_element and
// wait_for_element return.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PointerOffset nudges a resolved PointerTarget anchor by a fixed
// number of points, positive Dx rightward and positive Dy downward.
type PointerOffset struct {
	Dx *float64 `json:"dx,omitempty"`
	Dy *float64 `json:"dy,omitempty"`
}

// PointerTarget is the target of a pointer down or move action:
// either a selector-relative point (Selector, with optional Anchor
// and Offset) or an absolute Point. Exactly one form is populated;
// ParseScript never produces a value with both or neither set.
type PointerTarget struct {
	Selector *ScriptSelector `json:"selector,omitempty"`
	Anchor   *string         `json:"anchor,omitempty"`
	Offset   *PointerOffset  `json:"offset,omitempty"`
	Point    *Point          `json:"point,omitempty"`
}

// PointerAction is one primitive in a pointer gesture sequence,
// following W3C Actions semantics: Type is one of "down", "move",
// "wait", "up", or "capture". A "capture" action takes a screenshot
// while the pointer is held down, mid-gesture, and is valid only
// strictly between the sequence's opening "down" and its closing
// "up". Label names the artifact a "capture" action produces in the
// run report; it is ignored on every other Type.
type PointerAction struct {
	Type     string         `json:"type"`
	Target   *PointerTarget `json:"target,omitempty"`
	Pressure *float64       `json:"pressure,omitempty"`
	Ms       *float64       `json:"ms,omitempty"`
	Label    *string        `json:"label,omitempty"`
}

// PointerTouch is one pointer's path within a pointer step: an
// optional author-chosen ID and a required time-ordered action list.
// A step with one touch is a single-pointer gesture; a step with more
// than one touch is a multi-pointer gesture whose touches share a wall
// clock anchored at the first down. ID is optional for a single-touch
// gesture and required, unique, for a multi-pointer one so the driver
// can tell the concurrent paths apart.
type PointerTouch struct {
	ID      string          `json:"id,omitempty"`
	Actions []PointerAction `json:"actions"`
}

// StringOrSelector holds either a bare string shorthand or a full
// ScriptSelector, matching the `visible` / `notVisible` union that
// HandleCondition and PreconditionCheck both accept.
type StringOrSelector struct {
	Str      *string
	Selector *ScriptSelector
}

// MarshalJSON renders the populated alternative directly, with no
// wrapper object, so the JSON is either a bare string or a bare
// selector object, matching what a script author wrote for `visible`
// or `notVisible`.
func (v StringOrSelector) MarshalJSON() ([]byte, error) {
	if v.Str != nil {
		return json.Marshal(*v.Str)
	}
	if v.Selector != nil {
		return json.Marshal(v.Selector)
	}
	return []byte("null"), nil
}

// StepsOrFile holds either an inline step list or a path to a flow
// fragment file, matching the `do` / `setup` / `default` union every
// branching construct (handle, branch, precondition) accepts.
type StepsOrFile struct {
	Steps []ScriptStep
	File  *string
}

// MarshalJSON renders the populated alternative directly: the file
// path string, or the step array (as "[]" when empty rather than the
// JSON null a nil slice would otherwise produce).
func (v StepsOrFile) MarshalJSON() ([]byte, error) {
	if v.File != nil {
		return json.Marshal(*v.File)
	}
	if v.Steps == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(v.Steps)
}
