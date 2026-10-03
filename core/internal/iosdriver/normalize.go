package iosdriver

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"dev.atomyx/internal/wire"
)

// iosBounds is the raw rectangle shape the Swift driver reports on an
// element snapshot, expressed in the same left/top/right/bottom
// vocabulary as wire.Bounds.
type iosBounds struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// IosRawElement is the raw tree shape emitted by the Swift driver's
// dumpRawTree command, mirroring the JSON produced from an
// XCUIElementSnapshot walk. Every field here plus NormalizeIosTree
// together define the single mapping from the iOS snapshot vocabulary
// to the canonical wire.TreeNodeWire shape.
type IosRawElement struct {
	// ElementType is a pointer because an explicit empty string is a
	// value distinct from an absent field: NormalizeIosTree preserves
	// an explicit "" verbatim in the class attribute, and only an
	// absent field defaults to "other".
	ElementType        *string         `json:"elementType"`
	Identifier         string          `json:"identifier"`
	Label              string          `json:"label"`
	Value              string          `json:"value"`
	PlaceholderValue   string          `json:"placeholderValue"`
	Enabled            *bool           `json:"enabled"`
	Focused            *bool           `json:"focused"`
	Visible            *bool           `json:"visible"`
	Accessible         *bool           `json:"accessible"`
	Selected           *bool           `json:"selected"`
	Title              string          `json:"title"`
	Traits             []string        `json:"traits"`
	Bounds             *iosBounds      `json:"bounds"`
	AccessibilityFrame *iosBounds      `json:"accessibilityFrame"`
	Children           []IosRawElement `json:"children"`
}

// dumpRawTreeResponse is the payload shape of the dumpRawTree command:
// the snapshot root plus the bundle id of the app it was captured
// from, when the Swift driver was able to resolve one.
type dumpRawTreeResponse struct {
	BundleID string         `json:"bundleId"`
	Root     *IosRawElement `json:"root"`
}

// interactiveIosRoles is the whitelist of iOS element types treated
// as clickable when no accessibility trait says otherwise. iOS has no
// direct equivalent to Android's clickable bit, so a raw elementType
// whitelist is the fallback signal.
var interactiveIosRoles = map[string]bool{
	"button":          true,
	"cell":            true,
	"link":            true,
	"textField":       true,
	"secureTextField": true,
	"searchField":     true,
	"switch":          true,
	"slider":          true,
	"picker":          true,
	"tab":             true,
	"key":             true,
}

// Tuning knobs for the merged Flutter or custom card detector. See
// isMergedSemanticLeaf for the rationale of each cutoff.
const (
	imageLabelMaxChars    = 40
	minMergedLines        = 3
	shortFieldAvgChars    = 30
	mergedLeafMinHeightPX = 70
)

// isMergedSemanticLeaf decides whether an iOS leaf with elementType
// and label is in fact a merged composite (a Flutter card whose
// children were collapsed by the accessibility engine) rather than a
// real image or text element.
//
// The function only returns true for leaves (childCount == 0). If
// XCUITest reports descendants the raw role is trusted as is, because
// a real container with children is obviously not being merged at
// this level.
//
// Bounds-driven detection runs first because it works without a
// label: a Flutter card often carries a non-empty label but the
// discriminator is geometric, not textual. A real iOS staticText leaf
// occupies one line of system font; a merged composite (icon-on-top
// plus label-below in a card) reports as a single leaf whose bounds
// height covers the full card region. mergedLeafMinHeightPX is the
// height, in points, above which a staticText or image leaf is
// treated as a merged composite rather than a literal text or image
// element.
func isMergedSemanticLeaf(elementType, label string, childCount int, bounds *iosBounds) bool {
	if childCount > 0 {
		return false
	}
	if (elementType == "staticText" || elementType == "image") && bounds != nil {
		heightPx := bounds.Bottom - bounds.Top
		if heightPx >= mergedLeafMinHeightPX {
			return true
		}
	}
	if label == "" {
		return false
	}
	newlineCount := strings.Count(label, "\n")

	if elementType == "image" {
		return utf16Len(label) > imageLabelMaxChars || newlineCount >= 1
	}

	if elementType == "staticText" {
		if newlineCount < minMergedLines {
			return false
		}
		avgLineLength := float64(utf16Len(label)) / float64(newlineCount+1)
		return avgLineLength < shortFieldAvgChars
	}

	return false
}

// utf16Len counts UTF-16 code units in s, the same unit JavaScript's
// String.prototype.length measures. A rune outside the Basic
// Multilingual Plane (for example an emoji) encodes as a two-unit
// surrogate pair in UTF-16, so it counts as two here, which keeps the
// merged-leaf thresholds tripping at the label length the on-device
// agents expect.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if l := utf16.RuneLen(r); l > 0 {
			n += l
		} else {
			n++
		}
	}
	return n
}

// iosElementTypeToRole maps an iOS XCUIElement.ElementType string to
// a canonical cross-platform role.
func iosElementTypeToRole(elementType string) string {
	switch elementType {
	case "button", "popupButton":
		return "button"
	case "link":
		return "link"
	case "textField":
		return "text-field"
	case "secureTextField":
		return "secure-text-field"
	case "searchField":
		return "search-field"
	case "staticText":
		return "text"
	case "cell":
		return "cell"
	case "image", "icon":
		return "image"
	case "switch", "toggle":
		return "switch"
	case "slider":
		return "slider"
	case "checkBox":
		return "checkbox"
	case "radioButton":
		return "radio-button"
	case "navigationBar", "tabBar", "tabGroup", "toolbar", "menuBar", "menu":
		return "menu"
	case "alert":
		return "alert"
	case "dialog", "sheet":
		return "dialog"
	case "keyboard":
		return "keyboard"
	case "key":
		return "key"
	case "table", "collectionView", "scrollView", "group", "window", "splitGroup", "stackView", "layoutArea":
		return "container"
	case "heading":
		return "heading"
	case "pickerWheel", "picker":
		return "slider"
	case "activityIndicator":
		return "activityindicator"
	case "progressIndicator":
		return "progressindicator"
	case "other":
		return "other"
	default:
		return "other"
	}
}

// traitsToRole maps a decoded UIAccessibilityTraits set to a
// canonical role. It reports ok false when the trait set is empty or
// carries no role-defining bits, in which case the caller falls back
// to the elementType mapping.
//
// Decision priority, most specific first: searchField, keyboardKey,
// header, link, button-and-image (a visual button plus image often
// signals a card so it is treated as a container), button,
// image-and-staticText (a Flutter merge of icon and text into one
// leaf, treated as a container), image, staticText.
func traitsToRole(traits []string) (string, bool) {
	if len(traits) == 0 {
		return "", false
	}
	has := func(t string) bool {
		for _, x := range traits {
			if x == t {
				return true
			}
		}
		return false
	}
	switch {
	case has("searchField"):
		return "search-field", true
	case has("keyboardKey"):
		return "key", true
	case has("header"):
		return "heading", true
	case has("link"):
		return "link", true
	case has("button") && has("image"):
		return "container", true
	case has("button"):
		return "button", true
	case has("image") && has("staticText"):
		return "container", true
	case has("image"):
		return "image", true
	case has("staticText"):
		return "text", true
	default:
		return "", false
	}
}

// formatBounds serializes a rectangle to the canonical "l,t,r,b"
// string the wire schema uses for the bounds attribute, trimming
// trailing zeros the way a JSON-number-to-string coercion would.
//
// Negative zero is normalized to positive zero before formatting:
// Go's strconv.FormatFloat renders -0 as "-0", but the canonical wire
// format never emits a signed zero, so callers can compare bounds
// strings byte-for-byte.
func formatBounds(b iosBounds) string {
	f := func(v float64) string {
		if v == 0 && math.Signbit(v) {
			v = 0
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return f(b.Left) + "," + f(b.Top) + "," + f(b.Right) + "," + f(b.Bottom)
}

// NormalizeIosTree translates one raw iOS element, recursively, into
// the canonical wire.TreeNodeWire shape.
//
// Attribute mapping (iOS to canonical):
//
//   - identifier                      -> attributes.id
//   - label (mirrored when the role is text or heading) -> attributes.text
//   - label (every other role)        -> attributes.label
//   - value                           -> attributes.value, and
//     overrides attributes.text when present, matching what an agent
//     sees as the current content of a text field.
//   - placeholderValue                -> attributes.hint
//   - elementType                     -> attributes.class
//   - (derived from elementType, traits, and the merged-leaf
//     heuristic)                      -> attributes.role
//   - bounds                          -> attributes.bounds
//   - elementType secureTextField     -> attributes.secure ("true")
//
// Role is decided in priority order: the keyboard elementType always
// wins, because Orchestra's keyboard-dismiss gate hard-codes role ==
// "keyboard"; then accessibility traits, which survive Flutter's
// accessibility-node merging that elementType alone cannot see
// through; then the raw elementType; then, as a last resort for
// image or text leaves with no traits, the label-shape heuristic in
// isMergedSemanticLeaf.
//
// clickable is derived rather than read directly: trait button or
// link is the strongest signal, because a Flutter button merged into
// a staticText leaf is still tappable and the trait carries that;
// otherwise it falls back to the interactiveIosRoles whitelist for
// legacy or no-trait dumps.
func NormalizeIosTree(raw IosRawElement) wire.TreeNodeWire {
	attributes := make(map[string]string)

	elementType := "other"
	if raw.ElementType != nil {
		elementType = *raw.ElementType
	}
	attributes["class"] = elementType

	childCount := len(raw.Children)
	var role string
	if elementType == "keyboard" {
		role = "keyboard"
	} else {
		if fromTraits, ok := traitsToRole(raw.Traits); ok {
			role = fromTraits
		} else if isMergedSemanticLeaf(elementType, raw.Label, childCount, raw.Bounds) {
			role = "container"
		} else {
			role = iosElementTypeToRole(elementType)
		}
		if (role == "text" || role == "image") && isMergedSemanticLeaf(elementType, raw.Label, childCount, raw.Bounds) {
			role = "container"
		}
	}
	attributes["role"] = role

	if raw.Identifier != "" {
		attributes["id"] = raw.Identifier
	}
	if raw.Label != "" {
		if role == "text" || role == "heading" {
			attributes["text"] = raw.Label
		} else {
			attributes["label"] = raw.Label
		}
	}
	if raw.Value != "" {
		attributes["value"] = raw.Value
		attributes["text"] = raw.Value
	}
	if raw.PlaceholderValue != "" {
		attributes["hint"] = raw.PlaceholderValue
	}
	if raw.Bounds != nil {
		attributes["bounds"] = formatBounds(*raw.Bounds)
	}
	if elementType == "secureTextField" {
		attributes[wire.AttrSecure] = wire.AttrSecureTrue
	}
	if raw.Title != "" {
		attributes["ext:ios-title"] = raw.Title
	}
	if len(raw.Traits) > 0 {
		attributes["ext:ios-traits"] = strings.Join(raw.Traits, ",")
	}
	if raw.AccessibilityFrame != nil {
		attributes["ext:ios-a11y-bounds"] = formatBounds(*raw.AccessibilityFrame)
	}
	if raw.Accessible != nil {
		attributes["ext:ios-accessible"] = strconv.FormatBool(*raw.Accessible)
	}

	children := make([]wire.TreeNodeWire, 0, childCount)
	for _, c := range raw.Children {
		children = append(children, NormalizeIosTree(c))
	}

	node := wire.TreeNodeWire{
		Attributes: attributes,
		Children:   children,
		Enabled:    raw.Enabled,
		Focused:    raw.Focused,
		Selected:   raw.Selected,
		Visible:    raw.Visible,
	}
	clickable := hasTrait(raw.Traits, "button") || hasTrait(raw.Traits, "link") || interactiveIosRoles[elementType]
	node.Clickable = &clickable

	return node
}

func hasTrait(traits []string, want string) bool {
	for _, t := range traits {
		if t == want {
			return true
		}
	}
	return false
}

func stringPtr(s string) *string { return &s }
