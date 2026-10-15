package orchestra

import (
	"testing"

	"dev.atomyx/internal/wire"
)

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func lit(s string) *Pattern {
	p := LiteralPattern(s)
	return &p
}

func testNode(attrs map[string]string, enabled, clickable, focused *bool, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: attrs,
		Children:   children,
		Enabled:    enabled,
		Clickable:  clickable,
		Focused:    focused,
	}
}

func priorityTestTree() wire.TreeNodeWire {
	loginBtn := testNode(
		map[string]string{AttrRole: "button", AttrID: "login_btn", AttrText: "Sign in", AttrLabel: "Login button"},
		boolPtr(true), boolPtr(true), nil,
	)
	cancelBtn := testNode(
		map[string]string{AttrRole: "button", AttrID: "cancel_btn", AttrText: "Cancel"},
		boolPtr(true), boolPtr(true), nil,
	)
	cell := testNode(map[string]string{AttrRole: "cell", AttrText: "Sign in"}, nil, nil, nil)
	email := testNode(
		map[string]string{AttrRole: "text-field", AttrID: "email", AttrHint: "Email address"},
		boolPtr(true), nil, nil,
	)
	return testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, loginBtn, cancelBtn, cell, email)
}

func idOf(c *TreeCursor) string   { return c.Node.Attributes[AttrID] }
func roleOf(c *TreeCursor) string { return c.Node.Attributes[AttrRole] }

func TestCompileSelector_MatchesByID(t *testing.T) {
	f := CompileSelector(Selector{ID: lit("login_btn")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "login_btn" {
		t.Fatalf("got %d matches, want 1 login_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_FallsBackToText(t *testing.T) {
	f := CompileSelector(Selector{Text: lit("Cancel")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "cancel_btn" {
		t.Fatalf("got %d matches, want 1 cancel_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_AndsRoleConstraint(t *testing.T) {
	f := CompileSelector(Selector{Text: lit("Sign in"), Role: strPtr("button")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "login_btn" {
		t.Fatalf("got %d matches, want 1 login_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_IDWinsOverText(t *testing.T) {
	f := CompileSelector(Selector{ID: lit("login_btn"), Text: lit("nonexistent")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "login_btn" {
		t.Fatalf("got %d matches, want 1 login_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_FallsThroughOnMiss(t *testing.T) {
	f := CompileSelector(Selector{ID: lit("nope"), Text: lit("Cancel")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "cancel_btn" {
		t.Fatalf("got %d matches, want 1 cancel_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_LabelBetweenIDAndText(t *testing.T) {
	f := CompileSelector(Selector{Label: lit("Login button")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "login_btn" {
		t.Fatalf("got %d matches, want 1 login_btn: %+v", len(r), r)
	}
}

func TestCompileSelector_HintForInputs(t *testing.T) {
	f := CompileSelector(Selector{Hint: lit("Email address")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "email" {
		t.Fatalf("got %d matches, want 1 email: %+v", len(r), r)
	}
}

func TestCompileSelector_NthSelectsFromBroadened(t *testing.T) {
	f := CompileSelector(Selector{Text: lit("Sign in"), Nth: intPtr(1)})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || roleOf(r[0]) != "cell" {
		t.Fatalf("got %d matches, want 1 cell: %+v", len(r), r)
	}
}

func TestCompileSelector_ConstraintOnly(t *testing.T) {
	f := CompileSelector(Selector{Role: strPtr("button")})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 2 {
		t.Fatalf("got %d matches, want 2", len(r))
	}
}

func TestCompileSelector_RegexAcrossFields(t *testing.T) {
	p, err := regexPatternFromJS("^cancel$", "i")
	if err != nil {
		t.Fatalf("compiling regex: %v", err)
	}
	f := CompileSelector(Selector{Text: &p})
	r := f(FromTree(priorityTestTree()))
	if len(r) != 1 || idOf(r[0]) != "cancel_btn" {
		t.Fatalf("got %d matches, want 1 cancel_btn: %+v", len(r), r)
	}
}

func duplicateLabelTree() wire.TreeNodeWire {
	title := testNode(map[string]string{AttrRole: "text", AttrText: "Verify"}, nil, nil, nil)
	button := testNode(
		map[string]string{AttrRole: "button", AttrID: "verify_btn", AttrText: "Verify"},
		boolPtr(true), boolPtr(true), nil,
	)
	return testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, title, button)
}

func TestCompileSelector_RanksClickableAboveNonClickable(t *testing.T) {
	f := CompileSelector(Selector{Text: lit("Verify")})
	r := f(FromTree(duplicateLabelTree()))
	if len(r) != 2 {
		t.Fatalf("got %d matches, want 2", len(r))
	}
	if r[0].Node.Clickable == nil || !*r[0].Node.Clickable || idOf(r[0]) != "verify_btn" {
		t.Fatalf("expected clickable verify_btn first, got %+v", r[0])
	}
}

func TestCompileSelector_SkipsRankingWithNth(t *testing.T) {
	f := CompileSelector(Selector{Text: lit("Verify"), Nth: intPtr(0)})
	r := f(FromTree(duplicateLabelTree()))
	if len(r) != 1 || roleOf(r[0]) != "text" {
		t.Fatalf("expected the non-clickable title first in document order, got %+v", r)
	}
}

func TestCompileSelector_RanksFocusedAboveNonFocused(t *testing.T) {
	a := testNode(map[string]string{AttrRole: "text-field", AttrID: "a", AttrHint: "Email"}, nil, nil, nil)
	b := testNode(map[string]string{AttrRole: "text-field", AttrID: "b", AttrHint: "Email"}, nil, nil, boolPtr(true))
	tree := testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, a, b)

	f := CompileSelector(Selector{Hint: lit("Email")})
	r := f(FromTree(tree))
	if len(r) != 2 {
		t.Fatalf("got %d matches, want 2", len(r))
	}
	if r[0].Node.Focused == nil || !*r[0].Node.Focused || idOf(r[0]) != "b" {
		t.Fatalf("expected focused node b first, got %+v", r[0])
	}
}

func TestCompileSelector_StableSortOnEqualScore(t *testing.T) {
	a := testNode(map[string]string{AttrRole: "button", AttrID: "a", AttrText: "Click"}, nil, boolPtr(true), nil)
	b := testNode(map[string]string{AttrRole: "button", AttrID: "b", AttrText: "Click"}, nil, boolPtr(true), nil)
	tree := testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, a, b)

	f := CompileSelector(Selector{Text: lit("Click")})
	r := f(FromTree(tree))
	if len(r) != 2 || idOf(r[0]) != "a" || idOf(r[1]) != "b" {
		t.Fatalf("expected stable document order [a, b], got %+v", r)
	}
}

// regexPatternFromJS is a small test helper exposing compileJSRegex
// so these tests can build a Pattern the same way UnmarshalJSON
// would from a {regex, flags} wire value.
func regexPatternFromJS(source, flags string) (Pattern, error) {
	re, err := compileJSRegex(source, flags)
	if err != nil {
		return Pattern{}, err
	}
	return RegexPattern(re), nil
}

func hintChainTree() wire.TreeNodeWire {
	byHint := testNode(map[string]string{AttrRole: "text-field", AttrID: "by_hint", AttrHint: "Email"}, nil, nil, nil)
	byHintSegment := testNode(map[string]string{AttrRole: "text-field", AttrID: "by_hint_segment", AttrHint: "Email\nEnter email"}, nil, nil, nil)
	byLabel := testNode(map[string]string{AttrRole: "text-field", AttrID: "by_label", AttrLabel: "Email"}, nil, nil, nil)
	byText := testNode(map[string]string{AttrRole: "text", AttrID: "by_text", AttrText: "Email"}, nil, nil, nil)
	return testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, byHint, byHintSegment, byLabel, byText)
}

func matchIDs(t *testing.T, s Selector, nodes ...wire.TreeNodeWire) []string {
	t.Helper()
	root := testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, nodes...)
	var ids []string
	for _, c := range CompileSelector(s)(FromTree(root)) {
		ids = append(ids, idOf(c))
	}
	return ids
}

func TestCompileSelector_TextFallsBackToHint(t *testing.T) {
	node := testNode(map[string]string{AttrID: "f", AttrHint: "Email"}, nil, nil, nil)
	if got := matchIDs(t, Selector{Text: lit("Email")}, node); len(got) != 1 || got[0] != "f" {
		t.Fatalf("got %v, want [f]", got)
	}
}

func TestCompileSelector_TextFallsBackToHintSegment(t *testing.T) {
	node := testNode(map[string]string{AttrID: "f", AttrHint: "Email\nEnter email"}, nil, nil, nil)
	if got := matchIDs(t, Selector{Text: lit("Email")}, node); len(got) != 1 || got[0] != "f" {
		t.Fatalf("got %v, want [f]", got)
	}
}

func TestCompileSelector_TextLabelWinsOverHint(t *testing.T) {
	hint := testNode(map[string]string{AttrID: "by_hint", AttrHint: "Email"}, nil, nil, nil)
	label := testNode(map[string]string{AttrID: "by_label", AttrLabel: "Email"}, nil, nil, nil)
	if got := matchIDs(t, Selector{Text: lit("Email")}, hint, label); len(got) != 1 || got[0] != "by_label" {
		t.Fatalf("got %v, want [by_label]", got)
	}
}

func TestCompileSelector_TextWinsOverLabelAndHint(t *testing.T) {
	got := matchIDs(t, Selector{Text: lit("Email")}, hintChainTree().Children...)
	if len(got) != 1 || got[0] != "by_text" {
		t.Fatalf("got %v, want [by_text]", got)
	}
}

func TestCompileSelector_HintWholeValueBeatsSegmentInChain(t *testing.T) {
	got := matchIDs(t, Selector{Text: lit("Email")},
		testNode(map[string]string{AttrID: "segment", AttrHint: "Email\nEnter email"}, nil, nil, nil),
		testNode(map[string]string{AttrID: "whole", AttrHint: "Email"}, nil, nil, nil),
	)
	if len(got) != 1 || got[0] != "whole" {
		t.Fatalf("got %v, want [whole]", got)
	}
}

func TestCompileSelector_ExplicitHintGetsNoTextFallbackToHint(t *testing.T) {
	hint := testNode(map[string]string{AttrID: "by_hint", AttrHint: "Email"}, nil, nil, nil)
	got := matchIDs(t, Selector{Text: lit("Email"), Hint: lit("Other")}, hint)
	if len(got) != 0 {
		t.Fatalf("got %v, want no match", got)
	}
}
