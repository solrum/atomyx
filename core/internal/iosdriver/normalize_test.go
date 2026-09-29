package iosdriver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/wire"
)

func decodeRawElement(t *testing.T, jsonSrc string) IosRawElement {
	t.Helper()
	var raw IosRawElement
	if err := json.Unmarshal([]byte(jsonSrc), &raw); err != nil {
		t.Fatalf("decode raw sample: %v", err)
	}
	return raw
}

func boolPtr(b bool) *bool { return &b }

func TestNormalizeIosTree_MapsAttributesAndState(t *testing.T) {
	raw := decodeRawElement(t, `{
		"elementType": "button",
		"identifier": "loginButton",
		"label": "Log In",
		"bounds": {"left": 10, "top": 20, "right": 110, "bottom": 60},
		"enabled": true,
		"focused": false,
		"selected": false,
		"visible": true
	}`)

	got := NormalizeIosTree(raw)

	want := wire.TreeNodeWire{
		Attributes: map[string]string{
			"id":     "loginButton",
			"label":  "Log In",
			"class":  "button",
			"role":   "button",
			"bounds": "10,20,110,60",
		},
		Children:  []wire.TreeNodeWire{},
		Clickable: boolPtr(true),
		Enabled:   boolPtr(true),
		Focused:   boolPtr(false),
		Selected:  boolPtr(false),
		Visible:   boolPtr(true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeIosTree() = %+v, want %+v", got, want)
	}
}

func TestNormalizeIosTree_TextLeafMirrorsLabelIntoText(t *testing.T) {
	raw := decodeRawElement(t, `{"elementType": "staticText", "label": "Welcome back"}`)

	got := NormalizeIosTree(raw)
	if got.Attributes["text"] != "Welcome back" {
		t.Fatalf("Attributes[text] = %q, want %q", got.Attributes["text"], "Welcome back")
	}
	if _, hasLabel := got.Attributes["label"]; hasLabel {
		t.Fatal("Attributes[label] present for a text-role leaf, want only text")
	}
}

func TestNormalizeIosTree_ValueOverridesTextOnTextField(t *testing.T) {
	raw := decodeRawElement(t, `{
		"elementType": "textField",
		"label": "Email",
		"value": "user@example.com",
		"placeholderValue": "you@example.com"
	}`)

	got := NormalizeIosTree(raw)
	if got.Attributes["label"] != "Email" {
		t.Fatalf("Attributes[label] = %q, want %q", got.Attributes["label"], "Email")
	}
	if got.Attributes["value"] != "user@example.com" {
		t.Fatalf("Attributes[value] = %q, want %q", got.Attributes["value"], "user@example.com")
	}
	if got.Attributes["text"] != "user@example.com" {
		t.Fatalf("Attributes[text] = %q, want the value to override the label-derived text", got.Attributes["text"])
	}
	if got.Attributes["hint"] != "you@example.com" {
		t.Fatalf("Attributes[hint] = %q, want %q", got.Attributes["hint"], "you@example.com")
	}
}

func TestNormalizeIosTree_KeyboardElementTypeWinsRoleOverTraits(t *testing.T) {
	raw := decodeRawElement(t, `{"elementType": "keyboard", "traits": ["button"]}`)

	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "keyboard" {
		t.Fatalf("Attributes[role] = %q, want %q", got.Attributes["role"], "keyboard")
	}
}

func TestNormalizeIosTree_TraitsToRolePriority(t *testing.T) {
	cases := []struct {
		name   string
		traits []string
		want   string
	}{
		{"searchField wins first", []string{"searchField", "button"}, "search-field"},
		{"keyboardKey", []string{"keyboardKey"}, "key"},
		{"header", []string{"header"}, "heading"},
		{"link", []string{"link"}, "link"},
		{"button and image collapse to container", []string{"button", "image"}, "container"},
		{"button alone", []string{"button"}, "button"},
		{"image and staticText collapse to container", []string{"image", "staticText"}, "container"},
		{"image alone", []string{"image"}, "image"},
		{"staticText alone", []string{"staticText"}, "text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := IosRawElement{ElementType: stringPtr("other"), Traits: tc.traits}
			got := NormalizeIosTree(raw)
			if got.Attributes["role"] != tc.want {
				t.Fatalf("role = %q, want %q for traits %v", got.Attributes["role"], tc.want, tc.traits)
			}
		})
	}
}

func TestNormalizeIosTree_ElementTypeFallbackWhenNoTraits(t *testing.T) {
	raw := IosRawElement{ElementType: stringPtr("scrollView")}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q", got.Attributes["role"], "container")
	}
}

func TestNormalizeIosTree_MergedLeaf_TallStaticTextBecomesContainer(t *testing.T) {
	// Height of 70 or more collapses a staticText leaf into a
	// container: the merged-leaf threshold is an inclusive lower
	// bound, so exactly 70 must already trigger the demotion.
	raw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Label:       "Card title",
		Bounds:      &iosBounds{Left: 0, Top: 0, Right: 200, Bottom: 70},
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q for a 70pt-tall staticText leaf", got.Attributes["role"], "container")
	}

	shortRaw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Label:       "Card title",
		Bounds:      &iosBounds{Left: 0, Top: 0, Right: 200, Bottom: 69},
	}
	gotShort := NormalizeIosTree(shortRaw)
	if gotShort.Attributes["role"] != "text" {
		t.Fatalf("role = %q, want %q for a 69pt-tall staticText leaf", gotShort.Attributes["role"], "text")
	}
}

func TestNormalizeIosTree_MergedLeaf_TallImageBecomesContainer(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("image"),
		Bounds:      &iosBounds{Left: 0, Top: 0, Right: 200, Bottom: 150},
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q for a tall image leaf", got.Attributes["role"], "container")
	}
}

func TestNormalizeIosTree_MergedLeaf_LongImageLabelBecomesContainer(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("image"),
		Label:       "This is a very long accessibility label describing a whole card region",
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q for an image leaf with a long label", got.Attributes["role"], "container")
	}
}

func TestNormalizeIosTree_MergedLeaf_SurrogatePairLabelAtBoundaryStaysImage(t *testing.T) {
	// Each U+1F600 rune encodes as a two-unit UTF-16 surrogate pair, so
	// 20 of them measure exactly 40 UTF-16 code units: the inclusive
	// boundary of imageLabelMaxChars, which must not yet trip the
	// merged-leaf demotion.
	raw := IosRawElement{
		ElementType: stringPtr("image"),
		Label:       strings.Repeat("\U0001F600", 20),
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "image" {
		t.Fatalf("role = %q, want %q for a 40-UTF-16-unit surrogate-pair label", got.Attributes["role"], "image")
	}
}

func TestNormalizeIosTree_MergedLeaf_SurrogatePairLabelOverBoundaryBecomesContainer(t *testing.T) {
	// 21 surrogate-pair runes measure 42 UTF-16 code units (over the
	// 40-unit threshold) while still only 21 Go runes (under it), so
	// this only trips the merged-leaf demotion when the label is
	// measured in UTF-16 code units, matching the iOS driver's
	// TypeScript tree normalizer.
	raw := IosRawElement{
		ElementType: stringPtr("image"),
		Label:       strings.Repeat("\U0001F600", 21),
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q for a 42-UTF-16-unit surrogate-pair label", got.Attributes["role"], "container")
	}
}

func TestNormalizeIosTree_MergedLeaf_MultilineStaticTextWithShortLinesBecomesContainer(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Label:       "Icon\nTitle\nSubtitle\nRow",
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "container" {
		t.Fatalf("role = %q, want %q for a short-lined multiline staticText leaf", got.Attributes["role"], "container")
	}
}

func TestNormalizeIosTree_MergedLeaf_MultilineStaticTextWithLongLinesStaysText(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Label: "This is a long first line of real prose\n" +
			"And a long second line of real prose too\n" +
			"And a third long line of real prose again\n" +
			"And a fourth long line of real prose here",
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "text" {
		t.Fatalf("role = %q, want %q for a long-lined multiline staticText leaf", got.Attributes["role"], "text")
	}
}

func TestNormalizeIosTree_MergedLeaf_NotAppliedWhenChildrenPresent(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Label:       "Card title",
		Bounds:      &iosBounds{Left: 0, Top: 0, Right: 200, Bottom: 150},
		Children:    []IosRawElement{{ElementType: stringPtr("image")}},
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["role"] != "text" {
		t.Fatalf("role = %q, want %q: the merge heuristic must not apply to a node with children", got.Attributes["role"], "text")
	}
}

func TestNormalizeIosTree_ClickableFromInteractiveRoleWhitelist(t *testing.T) {
	raw := IosRawElement{ElementType: stringPtr("textField")}
	got := NormalizeIosTree(raw)
	if got.Clickable == nil || !*got.Clickable {
		t.Fatalf("Clickable = %v, want true for elementType textField", got.Clickable)
	}
}

func TestNormalizeIosTree_ClickableFromButtonTraitEvenWhenMerged(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("staticText"),
		Traits:      []string{"button"},
		Bounds:      &iosBounds{Left: 0, Top: 0, Right: 100, Bottom: 150},
	}
	got := NormalizeIosTree(raw)
	if got.Clickable == nil || !*got.Clickable {
		t.Fatalf("Clickable = %v, want true when trait button is present", got.Clickable)
	}
}

func TestNormalizeIosTree_NotClickableForPlainContainer(t *testing.T) {
	raw := IosRawElement{ElementType: stringPtr("group")}
	got := NormalizeIosTree(raw)
	if got.Clickable == nil || *got.Clickable {
		t.Fatalf("Clickable = %v, want false for a plain group", got.Clickable)
	}
}

func TestNormalizeIosTree_ChildrenRecurseAndBoundsFormat(t *testing.T) {
	raw := IosRawElement{
		ElementType: stringPtr("window"),
		Children: []IosRawElement{
			{ElementType: stringPtr("button"), Identifier: "a", Bounds: &iosBounds{Left: 1, Top: 2.5, Right: 3, Bottom: 4}},
			{ElementType: stringPtr("cell"), Identifier: "b"},
		},
	}
	got := NormalizeIosTree(raw)
	if len(got.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(got.Children))
	}
	if got.Children[0].Attributes["bounds"] != "1,2.5,3,4" {
		t.Fatalf("Children[0].Attributes[bounds] = %q, want %q", got.Children[0].Attributes["bounds"], "1,2.5,3,4")
	}
	if got.Children[1].Attributes["id"] != "b" {
		t.Fatalf("Children[1].Attributes[id] = %q, want %q", got.Children[1].Attributes["id"], "b")
	}
}

func TestNormalizeIosTree_ExtensionAttributes(t *testing.T) {
	raw := IosRawElement{
		ElementType:        stringPtr("cell"),
		Title:              "Row title",
		Traits:             []string{"button", "selected"},
		AccessibilityFrame: &iosBounds{Left: 0, Top: 0, Right: 10, Bottom: 10},
		Accessible:         boolPtr(true),
	}
	got := NormalizeIosTree(raw)
	if got.Attributes["ext:ios-title"] != "Row title" {
		t.Fatalf("Attributes[ext:ios-title] = %q, want %q", got.Attributes["ext:ios-title"], "Row title")
	}
	if got.Attributes["ext:ios-traits"] != "button,selected" {
		t.Fatalf("Attributes[ext:ios-traits] = %q, want %q", got.Attributes["ext:ios-traits"], "button,selected")
	}
	if got.Attributes["ext:ios-a11y-bounds"] != "0,0,10,10" {
		t.Fatalf("Attributes[ext:ios-a11y-bounds] = %q, want %q", got.Attributes["ext:ios-a11y-bounds"], "0,0,10,10")
	}
	if got.Attributes["ext:ios-accessible"] != "true" {
		t.Fatalf("Attributes[ext:ios-accessible] = %q, want %q", got.Attributes["ext:ios-accessible"], "true")
	}
}
