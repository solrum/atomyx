package orchestra

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

// WaitTimeoutError is returned by WaitUntil when the deadline elapses
// before predicate is satisfied.
type WaitTimeoutError struct {
	Kind      string
	TimeoutMs int
}

func (e *WaitTimeoutError) Error() string {
	return fmt.Sprintf("wait timed out after %dms: %s", e.TimeoutMs, e.Kind)
}

// Code implements diag.Coded.
func (e *WaitTimeoutError) Code() string { return diag.CodeToolTimeout }

// Remedy implements diag.Coded.
func (e *WaitTimeoutError) Remedy() string {
	return "Re-orient with a fresh read of the screen, then retry the action."
}

var _ diag.Coded = (*WaitTimeoutError)(nil)

// WaitUntil is the generic poll-until-predicate primitive every
// observation-driven wait in this package specializes. It fetches a
// value, and if predicate does not accept it, sleeps intervalMs and
// fetches again, until predicate accepts a value or timeoutMs
// elapses from the first fetch. The first fetch always happens, even
// when timeoutMs is zero: a caller that wants "check now, don't
// wait" gets exactly that, returning the first fetched value when it
// satisfies predicate and a WaitTimeoutError otherwise.
func WaitUntil[T any](
	ctx context.Context,
	clock Clock,
	timeoutMs, intervalMs int,
	kind string,
	fetch func(context.Context) (T, error),
	predicate func(T) bool,
) (T, error) {
	deadline := clock.NowMs() + int64(timeoutMs)
	var last T
	for {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		value, err := fetch(ctx)
		if err != nil {
			return last, err
		}
		last = value
		if predicate(value) {
			return value, nil
		}
		remaining := deadline - clock.NowMs()
		if remaining <= 0 {
			break
		}
		sleepMs := int64(intervalMs)
		if remaining < sleepMs {
			sleepMs = remaining
		}
		if err := clock.Sleep(ctx, sleepMs); err != nil {
			return last, err
		}
	}
	return last, &WaitTimeoutError{Kind: kind, TimeoutMs: timeoutMs}
}

// KeyboardState is the on-screen keyboard's observable state, derived
// from a hierarchy snapshot.
type KeyboardState struct {
	Visible bool
	Bounds  *wire.Bounds
}

func readKeyboardState(tree wire.TreeNodeWire) KeyboardState {
	kb, found := findKeyboardNode(tree)
	if !found {
		return KeyboardState{Visible: false}
	}
	if bounds, ok := ParseBounds(kb.Node.Attributes[AttrBounds]); ok {
		return KeyboardState{Visible: true, Bounds: &bounds}
	}
	return KeyboardState{Visible: true}
}

func findKeyboardNode(tree wire.TreeNodeWire) (*TreeCursor, bool) {
	for _, c := range Walk(tree) {
		if isKeyboardNode(c.Node) {
			return c, true
		}
	}
	return nil, false
}

// findFocusedNode returns the first cursor in tree, in walk order,
// whose node reports input focus.
func findFocusedNode(tree wire.TreeNodeWire) (*TreeCursor, bool) {
	for _, c := range Walk(tree) {
		if c.Node.Focused != nil && *c.Node.Focused {
			return c, true
		}
	}
	return nil, false
}

func isKeyboardNode(n wire.TreeNodeWire) bool {
	if n.Attributes[AttrRole] == RoleKeyboard {
		return true
	}
	return n.Attributes["ext:isIme"] == "true"
}

// waitForKeyboard polls d's hierarchy until the on-screen keyboard is
// visible or hidden, as declared by expectVisible, or timeoutMs
// elapses. Used after Driver.HideKeyboard to confirm the dismissal
// animation completed before Orchestra interacts with elements below
// the former keyboard frame.
func waitForKeyboard(
	ctx context.Context,
	d HierarchyDriver,
	clock Clock,
	expectVisible bool,
	timeoutMs, intervalMs int,
) (KeyboardState, error) {
	kind := fmt.Sprintf("waitForKeyboard(visible=%t)", expectVisible)
	return WaitUntil(ctx, clock, timeoutMs, intervalMs, kind,
		func(ctx context.Context) (KeyboardState, error) {
			tree, err := d.Hierarchy(ctx)
			if err != nil {
				return KeyboardState{}, err
			}
			return readKeyboardState(tree), nil
		},
		func(s KeyboardState) bool { return s.Visible == expectVisible },
	)
}

// WaitForTargetFocus and WaitForInputReady below gate Orchestra's type
// actions: InputText waits on WaitForTargetFocus so text lands only once
// the tapped target actually holds focus, and TypeText waits on
// WaitForInputReady so it dispatches only once a field is ready. The
// remaining primitives (WaitForInputCommitted, WaitForText,
// WaitForTreeStable) have no Orchestra caller yet; they exist so this
// package's observation-driven wait surface stays complete for callers
// that compose their own post-action verification.

// focusLayoutShiftMaxPx bounds how far a field may move vertically
// between the pre-tap snapshot and the moment it takes focus. Opening
// the soft keyboard scrolls or resizes the layout so the focused field
// stays visible, and the keyboard is the largest thing that can push a
// field, so its height (about 400 px on a phone) is the ceiling.
const focusLayoutShiftMaxPx = 400

// FocusTarget identifies the node a type action resolved before it
// tapped, so the focus wait can confirm focus on that same node
// instead of re-running the selector against a tree whose labels and
// layout the tap may have changed.
type FocusTarget struct {
	// ID is the resolved node's identifier, empty when it has none.
	ID string
	// Bounds is the resolved node's pre-tap frame.
	Bounds wire.Bounds
	// otherBounds holds the pre-tap frame of every other node, so a
	// focused node sitting exactly on a different node's old frame is
	// recognized as that other node rather than a shifted target.
	otherBounds map[wire.Bounds]struct{}
}

// NewFocusTarget captures the pre-tap identity of cursor, which must
// carry parseable bounds, from the tree it points into.
func NewFocusTarget(cursor *TreeCursor, bounds wire.Bounds) FocusTarget {
	target := FocusTarget{
		ID:          cursor.Node.Attributes[AttrID],
		Bounds:      bounds,
		otherBounds: make(map[wire.Bounds]struct{}),
	}
	for _, c := range Walk(RootOf(cursor)) {
		if b, ok := ParseBounds(c.Node.Attributes[AttrBounds]); ok && b != bounds {
			target.otherBounds[b] = struct{}{}
		}
	}
	return target
}

// holdsFocus reports whether the focused node in tree is the target.
// Any one of these confirms it:
//
//   - the target has an id and a node with that id is the focused node
//     or shares its frame area
//   - the focused frame overlaps the target's pre-tap frame
//   - the focused frame overlaps the target horizontally, sits within
//     focusLayoutShiftMaxPx of it vertically, and is not exactly the
//     pre-tap frame of some other node (which would make it a
//     different field that already held that place)
func (t FocusTarget) holdsFocus(tree wire.TreeNodeWire) bool {
	focused, ok := findFocusedNode(tree)
	if !ok {
		return false
	}
	fb, ok := ParseBounds(focused.Node.Attributes[AttrBounds])
	if !ok {
		return false
	}
	if t.ID != "" {
		if focused.Node.Attributes[AttrID] == t.ID {
			return true
		}
		for _, c := range Walk(tree) {
			if c.Node.Attributes[AttrID] != t.ID {
				continue
			}
			if b, ok := ParseBounds(c.Node.Attributes[AttrBounds]); ok && BoundsIntersect(b, fb) {
				return true
			}
		}
	}
	if BoundsIntersect(t.Bounds, fb) {
		return true
	}
	if _, other := t.otherBounds[fb]; other {
		return false
	}
	horizontal := fb.Left < t.Bounds.Right && t.Bounds.Left < fb.Right
	shift := BoundsCenter(fb).Y - BoundsCenter(t.Bounds).Y
	return horizontal && shift <= focusLayoutShiftMaxPx && -shift <= focusLayoutShiftMaxPx
}

// WaitForTargetFocusOptions configures WaitForTargetFocus.
type WaitForTargetFocusOptions struct {
	Driver     HierarchyDriver
	Target     FocusTarget
	Clock      Clock
	TimeoutMs  int
	IntervalMs int
}

// WaitForTargetFocus polls Driver's hierarchy until the node captured
// in Target holds input focus, returning WaitTimeoutError when it never
// does. It does not re-run a selector, so a field
// whose label or hint changes when focused is still recognized, and a
// different field taking focus is not mistaken for the target.
func WaitForTargetFocus(ctx context.Context, opts WaitForTargetFocusOptions) error {
	timeoutMs := opts.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 1500
	}
	intervalMs := opts.IntervalMs
	if intervalMs == 0 {
		intervalMs = 50
	}
	_, err := WaitUntil(ctx, opts.Clock, timeoutMs, intervalMs, "waitForFocus",
		func(ctx context.Context) (bool, error) {
			tree, err := opts.Driver.Hierarchy(ctx)
			if err != nil {
				return false, err
			}
			return opts.Target.holdsFocus(tree), nil
		},
		func(held bool) bool { return held },
	)
	return err
}

// WaitForInputReadyOptions configures WaitForInputReady.
type WaitForInputReadyOptions struct {
	Driver     HierarchyDriver
	Selector   Selector
	Clock      Clock
	TimeoutMs  int
	IntervalMs int
}

// WaitForInputReady polls Driver's hierarchy until the input field
// matching Selector is ready to receive text. It accepts platforms
// where a custom text input doesn't expose input focus through the
// accessibility tree at all.
//
// Either signal below satisfies the wait:
//
//   - A node reports focus AND its bounds intersect a
//     selector-matched candidate's bounds. The strict signal,
//     matching native input fields.
//   - The on-screen keyboard is visible AND no node reports focus.
//     The fallback for custom text inputs that never surface
//     focus: the keyboard appearing after a tap on the target is
//     material evidence that focus landed somewhere, and "no
//     conflicting focus" rules out a prior field still claiming it.
//
// When a node reports focus but it does not resolve to Selector's
// target, the wait keeps polling rather than falling through to the
// keyboard signal, which would otherwise accept a wrong-target focus
// as ready.
func WaitForInputReady(ctx context.Context, opts WaitForInputReadyOptions) error {
	timeoutMs := opts.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 1500
	}
	intervalMs := opts.IntervalMs
	if intervalMs == 0 {
		intervalMs = 50
	}
	filter := CompileSelector(opts.Selector)
	_, err := WaitUntil(ctx, opts.Clock, timeoutMs, intervalMs, "waitForInputReady",
		func(ctx context.Context) (bool, error) {
			tree, err := opts.Driver.Hierarchy(ctx)
			if err != nil {
				return false, err
			}
			candidates := filter(FromTree(tree))
			focused, hasFocused := findFocusedNode(tree)

			if hasFocused {
				focusedBounds, ok := ParseBounds(focused.Node.Attributes[AttrBounds])
				if ok {
					for _, c := range candidates {
						cb, ok := ParseBounds(c.Node.Attributes[AttrBounds])
						if ok && BoundsIntersect(cb, focusedBounds) {
							return true, nil
						}
					}
				}
				return false, nil
			}

			return readKeyboardState(tree).Visible, nil
		},
		func(ready bool) bool { return ready },
	)
	return err
}

// WaitForTextOptions configures WaitForText.
type WaitForTextOptions struct {
	Driver   HierarchyDriver
	Selector Selector
	// Matches reports whether a candidate's text attribute satisfies
	// the caller's expectation. MatchText and MatchTextPattern build
	// the two shapes TS callers pass: an exact string and a regexp.
	Matches    func(actual string) bool
	Clock      Clock
	TimeoutMs  int
	IntervalMs int
}

// MatchText builds a WaitForTextOptions.Matches predicate requiring
// an exact match against expected.
func MatchText(expected string) func(string) bool {
	return func(actual string) bool { return actual == expected }
}

// MatchTextPattern builds a WaitForTextOptions.Matches predicate
// requiring pattern to match the candidate's text attribute.
func MatchTextPattern(pattern *regexp.Regexp) func(string) bool {
	return func(actual string) bool { return pattern.MatchString(actual) }
}

// WaitForText polls Driver's hierarchy until the element matching
// Selector reports a "text" attribute Matches accepts, returning the
// matching cursor.
func WaitForText(ctx context.Context, opts WaitForTextOptions) (*TreeCursor, error) {
	timeoutMs := opts.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 1000
	}
	intervalMs := opts.IntervalMs
	if intervalMs == 0 {
		intervalMs = 50
	}
	filter := CompileSelector(opts.Selector)
	return WaitUntil(ctx, opts.Clock, timeoutMs, intervalMs, "waitForText",
		func(ctx context.Context) (*TreeCursor, error) {
			tree, err := opts.Driver.Hierarchy(ctx)
			if err != nil {
				return nil, err
			}
			for _, c := range filter(FromTree(tree)) {
				if opts.Matches(c.Node.Attributes[AttrText]) {
					return c, nil
				}
			}
			return nil, nil
		},
		func(c *TreeCursor) bool { return c != nil },
	)
}

// maxSafeInteger mirrors JavaScript's Number.MAX_SAFE_INTEGER, used
// as areaOf's sentinel for a cursor with unparsable bounds so it
// sorts last among intersecting candidates.
const maxSafeInteger = 9007199254740991

// WaitForInputCommittedOptions configures WaitForInputCommitted.
type WaitForInputCommittedOptions struct {
	Driver HierarchyDriver
	// AnchorBounds is the bounds of the field that was typed into,
	// captured before the driver's InputText call. It anchors
	// relocation of the field after a role or attribute transition
	// (for example a text field becoming a secure field once
	// obscuring kicks in) that a Selector-based lookup would miss.
	AnchorBounds wire.Bounds
	Expected     string
	Clock        Clock
	TimeoutMs    int
	IntervalMs   int
}

// WaitForInputCommitted verifies that the input anchored at
// AnchorBounds accepted Expected, picking a role-appropriate
// postcondition:
//
//   - A secure node (see IsSecure) renders masked characters, never
//     the original text, so it is verified by character count: typing N
//     characters must render N masked characters.
//   - Every other input role is verified by exact string match.
//
// Either role also accepts a weaker "typed change" signal, that is, the
// text attribute is non-empty and differs from both the label and the
// hint, used when the
// strong postcondition hasn't caught up yet, because some platforms
// lag the accessibility value behind the type dispatch by more than
// this wait's timeout on long strings.
func WaitForInputCommitted(ctx context.Context, opts WaitForInputCommittedOptions) (*TreeCursor, error) {
	timeoutMs := opts.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 1000
	}
	intervalMs := opts.IntervalMs
	if intervalMs == 0 {
		intervalMs = 50
	}
	expectedLength := utf8.RuneCountInString(opts.Expected)
	return WaitUntil(ctx, opts.Clock, timeoutMs, intervalMs, "waitForInputCommitted",
		func(ctx context.Context) (*TreeCursor, error) {
			tree, err := opts.Driver.Hierarchy(ctx)
			if err != nil {
				return nil, err
			}
			return findAnchoredInputWithValue(tree, opts.AnchorBounds, opts.Expected, expectedLength), nil
		},
		func(c *TreeCursor) bool { return c != nil },
	)
}

// findAnchoredInputWithValue finds the best-matching cursor in tree
// whose bounds intersect anchorBounds and whose current text
// satisfies matchesExpectation, preferring the smallest-area match
// so a container ancestor whose rect trivially contains the field
// never shadows the field itself.
func findAnchoredInputWithValue(
	tree wire.TreeNodeWire,
	anchorBounds wire.Bounds,
	expected string,
	expectedLength int,
) *TreeCursor {
	var intersecting []*TreeCursor
	for _, c := range FromTree(tree) {
		if !isInputRole(c.Node.Attributes[AttrRole]) {
			continue
		}
		b, ok := ParseBounds(c.Node.Attributes[AttrBounds])
		if !ok {
			continue
		}
		if BoundsIntersect(b, anchorBounds) {
			intersecting = append(intersecting, c)
		}
	}
	sort.SliceStable(intersecting, func(i, j int) bool {
		return areaOf(intersecting[i]) < areaOf(intersecting[j])
	})
	for _, c := range intersecting {
		if matchesInputExpectation(c, expected, expectedLength) {
			return c
		}
	}
	return nil
}

func isInputRole(role string) bool {
	return role == RoleTextField || role == RoleSecureTextField || role == RoleSearchField
}

func areaOf(c *TreeCursor) float64 {
	b, ok := ParseBounds(c.Node.Attributes[AttrBounds])
	if !ok {
		return maxSafeInteger
	}
	return (b.Right - b.Left) * (b.Bottom - b.Top)
}

// matchesInputExpectation reports whether cursor shows the typed
// text. An empty Android field reports its hint as its text, so a
// text equal to the hint never counts as a typed change, and it never
// satisfies the masked-count check of a secure field either.
func matchesInputExpectation(cursor *TreeCursor, expected string, expectedLength int) bool {
	attrs := cursor.Node.Attributes
	actual, label, hint := attrs[AttrText], attrs[AttrLabel], attrs[AttrHint]

	if IsSecure(cursor.Node) {
		if actual != hint && utf8.RuneCountInString(actual) == expectedLength {
			return true
		}
		return hasTypedChange(actual, label, hint)
	}

	if actual == expected {
		return true
	}
	return hasTypedChange(actual, label, hint)
}

func hasTypedChange(actual, label, hint string) bool {
	return actual != "" && actual != label && actual != hint
}

// WaitForTreeStableOptions configures WaitForTreeStable.
type WaitForTreeStableOptions struct {
	Driver HierarchyDriver
	// QuietMs is how long the tree's structural signature must stay
	// unchanged before the wait resolves.
	QuietMs    int
	Clock      Clock
	TimeoutMs  int
	IntervalMs int
}

// WaitForTreeStable polls Driver's hierarchy until its structural
// signature, including attributes, bounds, focus, clickable, and enabled
// state across every node, stops changing for QuietMs, returning
// the latest tree. A driver without a native idle primitive uses
// this as its idle substitute.
//
// A continuously mutating tree (a progress spinner, a shimmer
// placeholder) never satisfies the quiet window and this wait times
// out rather than returning early; a caller that wants "any tree
// will do" passes a short QuietMs to tolerate that.
func WaitForTreeStable(ctx context.Context, opts WaitForTreeStableOptions) (wire.TreeNodeWire, error) {
	timeoutMs := opts.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 3000
	}
	intervalMs := opts.IntervalMs
	if intervalMs == 0 {
		intervalMs = 100
	}
	lastHash := ""
	stableSince := int64(-1)
	kind := fmt.Sprintf("waitForTreeStable(quietMs=%d)", opts.QuietMs)
	return WaitUntil(ctx, opts.Clock, timeoutMs, intervalMs, kind,
		func(ctx context.Context) (wire.TreeNodeWire, error) {
			return opts.Driver.Hierarchy(ctx)
		},
		func(tree wire.TreeNodeWire) bool {
			hash := hashTreeSignature(tree)
			now := opts.Clock.NowMs()
			if hash != lastHash {
				lastHash = hash
				stableSince = now
				return false
			}
			return now-stableSince >= int64(opts.QuietMs)
		},
	)
}

// hashTreeSignature builds a deep structural identity string for
// tree: not a cryptographic hash, just a signature two structurally
// identical trees are guaranteed to share and two structurally
// different trees are practically guaranteed not to.
func hashTreeSignature(node wire.TreeNodeWire) string {
	var parts []string
	appendTreeSignature(node, &parts)
	return strings.Join(parts, "|")
}

func appendTreeSignature(node wire.TreeNodeWire, parts *[]string) {
	keys := make([]string, 0, len(node.Attributes))
	for k := range node.Attributes {
		keys = append(keys, k)
	}
	// Sorted so insertion order can't produce different signatures
	// for structurally identical trees.
	sort.Strings(keys)
	*parts = append(*parts, "[")
	for _, k := range keys {
		*parts = append(*parts, k, "=", node.Attributes[k], ";")
	}
	if node.Focused != nil && *node.Focused {
		*parts = append(*parts, "F;")
	}
	if node.Clickable != nil && *node.Clickable {
		*parts = append(*parts, "C;")
	}
	if node.Enabled != nil && !*node.Enabled {
		*parts = append(*parts, "D;")
	}
	for _, child := range node.Children {
		appendTreeSignature(child, parts)
	}
	*parts = append(*parts, "]")
}
