package orchestra

import (
	"regexp"
	"testing"
)

func segmentTree() []*TreeCursor {
	merged := testNode(map[string]string{AttrRole: "button", AttrID: "nav_products", AttrLabel: "Products\nBrowse our catalog"}, nil, nil, nil)
	password := testNode(map[string]string{AttrRole: "text-field", AttrID: "pw", AttrHint: "Password\nEnter password"}, nil, nil, nil)
	whole := testNode(map[string]string{AttrRole: "text", AttrID: "title", AttrLabel: "Products"}, nil, nil, nil)
	root := testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, merged, password, whole)
	return FromTree(root)
}

func TestSegmentMatching_MergedLabelFoundBySegment(t *testing.T) {
	cursors := segmentTree()[:2]
	r := CompileSelector(Selector{Label: lit("Products")})(cursors)
	if len(r) != 1 || idOf(r[0]) != "nav_products" {
		t.Fatalf("got %d matches, want nav_products", len(r))
	}
}

func TestSegmentMatching_MergedHintFoundBySegment(t *testing.T) {
	r := CompileSelector(Selector{Hint: lit("Password")})(segmentTree())
	if len(r) != 1 || idOf(r[0]) != "pw" {
		t.Fatalf("got %d matches, want pw", len(r))
	}
}

func TestSegmentMatching_TextSelectorFallsBackToLabelSegment(t *testing.T) {
	r := CompileSelector(Selector{Text: lit("Browse our catalog")})(segmentTree())
	if len(r) != 1 || idOf(r[0]) != "nav_products" {
		t.Fatalf("got %d matches, want nav_products", len(r))
	}
}

func TestSegmentMatching_WholeValueBeatsSegment(t *testing.T) {
	r := CompileSelector(Selector{Label: lit("Products")})(segmentTree())
	if len(r) != 1 || idOf(r[0]) != "title" {
		t.Fatalf("got %d matches (first %q), want only title", len(r), idOf(r[0]))
	}
}

func TestSegmentMatching_NoPartialSegmentOrSubstring(t *testing.T) {
	for _, v := range []string{"Prod", "Products\nBrowse", "Browse our", "products", "Products ", ""} {
		for _, c := range CompileSelector(Selector{Label: lit(v)})(segmentTree()[:2]) {
			t.Fatalf("label %q unexpectedly matched %q", v, idOf(c))
		}
	}
}

func TestSegmentMatching_RegexUnchanged(t *testing.T) {
	re := RegexPattern(regexp.MustCompile(`^Browse`))
	if r := CompileSelector(Selector{Label: &re})(segmentTree()); len(r) != 0 {
		t.Fatalf("regex anchored at a later segment matched %d nodes", len(r))
	}
	re = RegexPattern(regexp.MustCompile(`^Products`))
	if r := CompileSelector(Selector{Label: &re})(segmentTree()); len(r) != 2 {
		t.Fatalf("regex prefix matched %d nodes, want 2", len(r))
	}
}

func TestSegmentMatching_IDFallsBackToLabelSegmentLast(t *testing.T) {
	folded := testNode(map[string]string{AttrRole: "button", AttrID: "p1", AttrLabel: "p1\np1_tooltip\nx"}, nil, nil, nil)
	cursors := FromTree(testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, folded))
	r := CompileSelector(Selector{ID: lit("p1_tooltip")})(cursors)
	if len(r) != 1 || idOf(r[0]) != "p1" {
		t.Fatalf("got %d matches, want folded parent p1", len(r))
	}

	own := testNode(map[string]string{AttrRole: "text", AttrID: "p1_tooltip"}, nil, nil, nil)
	cursors = FromTree(testNode(map[string]string{AttrRole: "container"}, nil, nil, nil, folded, own))
	r = CompileSelector(Selector{ID: lit("p1_tooltip")})(cursors)
	if len(r) != 1 || r[0].Node.Attributes[AttrRole] != "text" {
		t.Fatalf("an own id node must win over a label segment")
	}
}

func TestSegmentMatching_ConstraintsApplyToSegmentStage(t *testing.T) {
	r := CompileSelector(Selector{Label: lit("Products"), Role: strPtr("button")})(segmentTree())
	if len(r) != 1 || idOf(r[0]) != "nav_products" {
		t.Fatalf("role constraint should reach the segment match, got %d", len(r))
	}
}

func TestPattern_MatchesSegment(t *testing.T) {
	p := LiteralPattern("b")
	cases := map[string]bool{"a\nb\nc": true, "b\nc": true, "a\nb": true, "b": false, "ab\nc": false, "": false}
	for v, want := range cases {
		if got := p.MatchesSegment(v); got != want {
			t.Errorf("MatchesSegment(%q) = %v, want %v", v, got, want)
		}
	}
	if LiteralPattern("").MatchesSegment("a\n\nb") {
		t.Error("empty literal must not match an empty segment")
	}
}
