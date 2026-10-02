package orchestra

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

// This file unit-tests the five wait primitives directly over an
// injected FakeClock and mock.Driver: WaitForFocus, WaitForInputReady,
// WaitForInputCommitted, WaitForText, and WaitForTreeStable.
// WaitForFocus and WaitForInputReady also gate Orchestra's InputText
// and TypeText, covered by the Orchestra tests; the other three have no
// Orchestra caller yet. There is no differential oracle for any of
// them, so these unit tests are the verification bar for this file.

func nodeWithRole(id, text, role, bounds string, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: map[string]string{AttrID: id, AttrText: text, AttrRole: role, AttrBounds: bounds},
		Children:   children,
	}
}

func focusedNode(id, text, bounds string) wire.TreeNodeWire {
	n := node(id, text, bounds)
	t := true
	n.Focused = &t
	return n
}

// ── BoundsIntersect ──────────────────────────────────────────────

func TestBoundsIntersect(t *testing.T) {
	tests := []struct {
		name string
		a, b wire.Bounds
		want bool
	}{
		{"overlapping", wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, wire.Bounds{Left: 5, Top: 5, Right: 15, Bottom: 15}, true},
		{"identical", wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, true},
		{"disjoint-horizontally", wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, wire.Bounds{Left: 10, Top: 0, Right: 20, Bottom: 10}, false},
		{"disjoint-vertically", wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, wire.Bounds{Left: 0, Top: 10, Right: 10, Bottom: 20}, false},
		{"touching-corner-only", wire.Bounds{Left: 0, Top: 0, Right: 10, Bottom: 10}, wire.Bounds{Left: 10, Top: 10, Right: 20, Bottom: 20}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BoundsIntersect(tc.a, tc.b); got != tc.want {
				t.Fatalf("BoundsIntersect(%+v, %+v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// ── hasTypedChange / matchesInputExpectation ────────────────────

func TestHasTypedChange(t *testing.T) {
	tests := []struct {
		name, actual, label string
		want                bool
	}{
		{"empty-actual", "", "Password", false},
		{"unchanged-from-label", "Password", "Password", false},
		{"changed", "abc", "Password", true},
		{"changed-from-empty-label", "abc", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasTypedChange(tc.actual, tc.label); got != tc.want {
				t.Fatalf("hasTypedChange(%q, %q) = %v, want %v", tc.actual, tc.label, got, tc.want)
			}
		})
	}
}

func TestMatchesInputExpectation(t *testing.T) {
	tests := []struct {
		name           string
		actual, label  string
		role           string
		secure         bool
		expected       string
		expectedLength int
		want           bool
	}{
		{"exact-match-text-field", "hello", "", RoleTextField, false, "hello", 5, true},
		{"typed-change-fallback-text-field", "h", "Placeholder", RoleTextField, false, "hello", 5, true},
		{"no-change-text-field", "Placeholder", "Placeholder", RoleTextField, false, "hello", 5, false},
		{"android-password-exact-mask-count", "•••••", "", RoleTextField, true, "hello", 5, true},
		{"android-password-weak-typed-change", "••", "Password", RoleTextField, true, "hello", 5, true},
		{"android-password-no-change", "Password", "Password", RoleTextField, true, "hello", 5, false},
		{"android-password-empty", "", "Password", RoleTextField, true, "hello", 5, false},
		{"ios-secure-exact-mask-count", "•••••", "", RoleSecureTextField, true, "hello", 5, true},
		{"ios-secure-no-change", "Password", "Password", RoleSecureTextField, true, "hello", 5, false},
		{"role-without-attribute-uses-exact-match", "•••••", "", RoleSecureTextField, false, "hello", 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := map[string]string{AttrText: tc.actual, AttrLabel: tc.label, AttrRole: tc.role}
			if tc.secure {
				attrs[AttrSecure] = wire.AttrSecureTrue
			}
			cursor := &TreeCursor{Node: wire.TreeNodeWire{Attributes: attrs}}
			if got := matchesInputExpectation(cursor, tc.expected, tc.expectedLength); got != tc.want {
				t.Fatalf("matchesInputExpectation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── hashTreeSignature ────────────────────────────────────────────

func TestHashTreeSignature_StableAcrossAttributeInsertionOrder(t *testing.T) {
	a := wire.TreeNodeWire{Attributes: map[string]string{AttrID: "x", AttrText: "y"}}
	b := wire.TreeNodeWire{Attributes: map[string]string{AttrText: "y", AttrID: "x"}}
	if hashTreeSignature(a) != hashTreeSignature(b) {
		t.Fatal("expected identical signatures regardless of map insertion order")
	}
}

func TestHashTreeSignature_DiffersOnTextChange(t *testing.T) {
	a := node("root", "Loading", "0,0,100,100")
	b := node("root", "Loaded", "0,0,100,100")
	if hashTreeSignature(a) == hashTreeSignature(b) {
		t.Fatal("expected different signatures for different text")
	}
}

func TestHashTreeSignature_DiffersOnFocusChange(t *testing.T) {
	plain := node("field", "", "0,0,100,40")
	focused := focusedNode("field", "", "0,0,100,40")
	if hashTreeSignature(plain) == hashTreeSignature(focused) {
		t.Fatal("expected different signatures when focus state changes")
	}
}

func TestHashTreeSignature_SameForStructurallyIdenticalTrees(t *testing.T) {
	a := node("root", "", "0,0,100,100", node("child", "hi", "0,0,50,50"))
	b := node("root", "", "0,0,100,100", node("child", "hi", "0,0,50,50"))
	if hashTreeSignature(a) != hashTreeSignature(b) {
		t.Fatal("expected identical signatures for structurally identical trees")
	}
}

// ── WaitForFocus ─────────────────────────────────────────────────

func TestWaitForFocus_ResolvesWhenBoundsIntersectFocusedNode(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", node("field", "", "10,10,110,50")))
	d.StageHierarchy(node("root", "", "0,0,430,932", focusedNode("field", "", "10,10,110,50")))
	clock := NewFakeClock(0)

	cursor, err := WaitForFocus(context.Background(), WaitForFocusOptions{
		Driver: d, Selector: Selector{ID: lit("field")}, Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForFocus returned error: %v", err)
	}
	if cursor.Node.Attributes[AttrID] != "field" {
		t.Fatalf("got cursor id %q, want %q", cursor.Node.Attributes[AttrID], "field")
	}
}

func TestWaitForFocus_TimesOutWhenNeverFocused(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", node("field", "", "10,10,110,50")))
	clock := NewFakeClock(0)

	_, err := WaitForFocus(context.Background(), WaitForFocusOptions{
		Driver: d, Selector: Selector{ID: lit("field")}, Clock: clock, TimeoutMs: 120, IntervalMs: 50,
	})
	var timeout *WaitTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("got error %v (%T), want *WaitTimeoutError", err, err)
	}
	if timeout.Kind != "waitForFocus" {
		t.Fatalf("got kind %q, want %q", timeout.Kind, "waitForFocus")
	}
}

// ── WaitForInputReady ────────────────────────────────────────────

func TestWaitForInputReady_SignalA_StrictFocusOnTarget(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", focusedNode("field", "", "10,10,110,50")))
	clock := NewFakeClock(0)

	err := WaitForInputReady(context.Background(), WaitForInputReadyOptions{
		Driver: d, Selector: Selector{ID: lit("field")}, Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForInputReady returned error: %v", err)
	}
}

func TestWaitForInputReady_SignalB_KeyboardFallbackWhenNoFocusReported(t *testing.T) {
	d := mock.New()
	// No node reports Focused=true; the on-screen keyboard node
	// stands in as evidence that focus landed somewhere.
	d.StageHierarchy(node("root", "", "0,0,430,932",
		node("field", "", "10,10,110,50"),
		nodeWithRole("kb", "", RoleKeyboard, "0,600,430,932"),
	))
	clock := NewFakeClock(0)

	err := WaitForInputReady(context.Background(), WaitForInputReadyOptions{
		Driver: d, Selector: Selector{ID: lit("field")}, Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForInputReady returned error: %v", err)
	}
}

func TestWaitForInputReady_WrongTargetFocusDoesNotFallThroughToKeyboard(t *testing.T) {
	d := mock.New()
	// A different field claims focus, and the keyboard is visible.
	// The strict-focus branch must return false without consulting
	// the keyboard fallback, so the wait keeps polling and times out.
	d.StageHierarchy(node("root", "", "0,0,430,932",
		node("field", "", "10,10,110,50"),
		focusedNode("other-field", "", "10,60,110,100"),
		nodeWithRole("kb", "", RoleKeyboard, "0,600,430,932"),
	))
	clock := NewFakeClock(0)

	err := WaitForInputReady(context.Background(), WaitForInputReadyOptions{
		Driver: d, Selector: Selector{ID: lit("field")}, Clock: clock, TimeoutMs: 120, IntervalMs: 50,
	})
	var timeout *WaitTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("got error %v (%T), want *WaitTimeoutError", err, err)
	}
}

// ── WaitForText ──────────────────────────────────────────────────

func TestWaitForText_ExactMatch(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", node("label", "Loading...", "0,0,100,40")))
	d.StageHierarchy(node("root", "", "0,0,430,932", node("label", "Done", "0,0,100,40")))
	clock := NewFakeClock(0)

	cursor, err := WaitForText(context.Background(), WaitForTextOptions{
		Driver: d, Selector: Selector{ID: lit("label")}, Matches: MatchText("Done"),
		Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForText returned error: %v", err)
	}
	if cursor.Node.Attributes[AttrText] != "Done" {
		t.Fatalf("got text %q, want %q", cursor.Node.Attributes[AttrText], "Done")
	}
}

func TestWaitForText_PatternMatch(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", node("label", "3 items left", "0,0,100,40")))
	clock := NewFakeClock(0)

	cursor, err := WaitForText(context.Background(), WaitForTextOptions{
		Driver: d, Selector: Selector{ID: lit("label")}, Matches: MatchTextPattern(regexp.MustCompile(`^\d+ items left$`)),
		Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForText returned error: %v", err)
	}
	if cursor.Node.Attributes[AttrID] != "label" {
		t.Fatalf("got id %q, want %q", cursor.Node.Attributes[AttrID], "label")
	}
}

// ── WaitForInputCommitted ────────────────────────────────────────

func TestWaitForInputCommitted_ExactMatchOnTextField(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "", "0,0,430,932", nodeWithRole("field", "", RoleTextField, "10,10,110,50")))
	d.StageHierarchy(node("root", "", "0,0,430,932", nodeWithRole("field", "hello", RoleTextField, "10,10,110,50")))
	clock := NewFakeClock(0)

	cursor, err := WaitForInputCommitted(context.Background(), WaitForInputCommittedOptions{
		Driver: d, AnchorBounds: wire.Bounds{Left: 10, Top: 10, Right: 110, Bottom: 50}, Expected: "hello",
		Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForInputCommitted returned error: %v", err)
	}
	if cursor.Node.Attributes[AttrText] != "hello" {
		t.Fatalf("got text %q, want %q", cursor.Node.Attributes[AttrText], "hello")
	}
}

func TestWaitForInputCommitted_SecureFieldVerifiesByMaskedCount(t *testing.T) {
	for _, role := range []string{RoleSecureTextField, RoleTextField} {
		t.Run(role, func(t *testing.T) {
			d := mock.New()
			field := nodeWithRole("field", "•••••", role, "10,10,110,50")
			field.Attributes[AttrSecure] = wire.AttrSecureTrue
			d.StageHierarchy(node("root", "", "0,0,430,932", field))
			clock := NewFakeClock(0)

			_, err := WaitForInputCommitted(context.Background(), WaitForInputCommittedOptions{
				Driver: d, AnchorBounds: wire.Bounds{Left: 10, Top: 10, Right: 110, Bottom: 50}, Expected: "hello",
				Clock: clock, TimeoutMs: 500, IntervalMs: 50,
			})
			if err != nil {
				t.Fatalf("WaitForInputCommitted returned error: %v", err)
			}
		})
	}
}

func TestWaitForInputCommitted_PrefersSmallestIntersectingArea(t *testing.T) {
	d := mock.New()
	// A large container-like text field intersects the anchor along
	// with the actual, smaller field. Only the smaller one carries
	// the expected value, so the loop must keep going past the
	// larger candidate rather than stopping at the first intersect.
	d.StageHierarchy(node("root", "", "0,0,430,932",
		nodeWithRole("wrapper", "", RoleTextField, "0,0,430,200"),
		nodeWithRole("field", "hello", RoleTextField, "10,10,110,50"),
	))
	clock := NewFakeClock(0)

	cursor, err := WaitForInputCommitted(context.Background(), WaitForInputCommittedOptions{
		Driver: d, AnchorBounds: wire.Bounds{Left: 10, Top: 10, Right: 110, Bottom: 50}, Expected: "hello",
		Clock: clock, TimeoutMs: 500, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForInputCommitted returned error: %v", err)
	}
	if cursor.Node.Attributes[AttrID] != "field" {
		t.Fatalf("got id %q, want %q", cursor.Node.Attributes[AttrID], "field")
	}
}

// ── WaitForTreeStable ────────────────────────────────────────────

func TestWaitForTreeStable_ResolvesAfterQuietWindow(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(node("root", "Loading", "0,0,430,932"))
	d.StageHierarchy(node("root", "Loaded", "0,0,430,932"))
	d.StageHierarchy(node("root", "Loaded", "0,0,430,932"))
	clock := NewFakeClock(0)

	tree, err := WaitForTreeStable(context.Background(), WaitForTreeStableOptions{
		Driver: d, QuietMs: 100, Clock: clock, TimeoutMs: 1000, IntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("WaitForTreeStable returned error: %v", err)
	}
	if tree.Attributes[AttrText] != "Loaded" {
		t.Fatalf("got text %q, want %q", tree.Attributes[AttrText], "Loaded")
	}
}

func TestWaitForTreeStable_TimesOutWhileTreeKeepsChanging(t *testing.T) {
	d := mock.New()
	clock := NewFakeClock(0)
	d.StageHierarchy(node("root", "0", "0,0,430,932"))
	for i := 1; i <= 10; i++ {
		d.StageHierarchy(node("root", string(rune('a'+i)), "0,0,430,932"))
	}

	_, err := WaitForTreeStable(context.Background(), WaitForTreeStableOptions{
		Driver: d, QuietMs: 100, Clock: clock, TimeoutMs: 300, IntervalMs: 50,
	})
	var timeout *WaitTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("got error %v (%T), want *WaitTimeoutError", err, err)
	}
}
