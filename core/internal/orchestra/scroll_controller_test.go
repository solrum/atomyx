package orchestra

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

// These tests exercise ScrollController.EnsureVisible directly
// against mock.Driver, covering the stateful scroll-search edges
// that orchestra_test.go's end-to-end Orchestra tests don't reach:
// the thin-tree budget cap, HashVisibleLeaves-based saturation
// break, the positional-loop "bounds unchanged" edge, and full
// scroll-search exhaustion when the tree keeps changing.

func newTestScrollController(d *mock.Driver, opts ScrollControllerOptions) (*ScrollController, *FakeClock) {
	clock := NewFakeClock(0)
	return NewScrollController(d, clock, opts), clock
}

func thinDecoyTree(i int) wire.TreeNodeWire {
	return node("root", "", "", node(fmt.Sprintf("decoy-%d", i), fmt.Sprintf("Decoy %d", i), "0,200,430,240"))
}

// fatTreeWithChangingLeaf returns a 5-leaf tree (at the thin-tree
// leaf threshold, so the viewport pre-check never caps its budget)
// with one leaf's text keyed by i so consecutive calls at different
// i hash differently and scrollSearch never saturates early.
func fatTreeWithChangingLeaf(i int) wire.TreeNodeWire {
	return node("root", "", "0,0,430,932",
		node(fmt.Sprintf("changing-%d", i), fmt.Sprintf("Changing %d", i), "0,100,430,140"),
		node("filler-a", "FillerA", "0,300,430,340"),
		node("filler-b", "FillerB", "0,400,430,440"),
		node("filler-c", "FillerC", "0,500,430,540"),
		node("filler-d", "FillerD", "0,600,430,640"),
	)
}

func swipeCount(d *mock.Driver) int {
	n := 0
	for _, c := range d.Calls {
		if c.Method == "Swipe" {
			n++
		}
	}
	return n
}

func TestScrollController_ThinTreeCapsScrollSearchBudget(t *testing.T) {
	d := mock.New()
	// initial find + scrollSearch pre-loop + up to 2 UP swipes' hierarchy
	// calls + up to 2 DOWN swipes' hierarchy calls, each distinct so
	// saturation never fires before the thin-tree cap does.
	for i := 0; i < 6; i++ {
		d.StageHierarchy(thinDecoyTree(i))
	}
	s, _ := newTestScrollController(d, ScrollControllerOptions{
		MaxIterations:      8,
		MaxScrollFraction:  0.6,
		SwipeDurationMs:    200,
		SettleWaitMs:       500,
		ScrollSearchBudget: 6,
		ViewportInsets:     DefaultInsets,
	})

	_, err := s.EnsureVisible(context.Background(), CompileSelector(Selector{ID: lit("target")}))
	var unreachable *ScrollUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("got error %v (%T), want *ScrollUnreachableError", err, err)
	}
	if !unreachable.NotFound {
		t.Fatal("an element that never appeared must report NotFound")
	}

	// Configured budget is 6 (12 total swipes), but the thin-tree
	// pre-check caps it to thinTreeBudget=2 per direction (4 total).
	if got := swipeCount(d); got != 4 {
		t.Fatalf("got %d swipes, want 4 (thin-tree budget cap of 2 per direction)", got)
	}
}

func TestScrollController_SaturationBreaksScrollSearchEarly(t *testing.T) {
	d := mock.New()
	// The same non-thin tree forever: HashVisibleLeaves never changes,
	// so scrollSearch's saturation check breaks each direction's loop
	// on its first swipe regardless of the configured budget.
	d.StageHierarchyRepeated(fatTreeWithChangingLeaf(0), 50)
	s, _ := newTestScrollController(d, ScrollControllerOptions{
		MaxIterations:      8,
		MaxScrollFraction:  0.6,
		SwipeDurationMs:    200,
		SettleWaitMs:       500,
		ScrollSearchBudget: 6,
		ViewportInsets:     DefaultInsets,
	})

	_, err := s.EnsureVisible(context.Background(), CompileSelector(Selector{ID: lit("target")}))
	var unreachable *ScrollUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("got error %v (%T), want *ScrollUnreachableError", err, err)
	}

	// Both directions saturate on their first swipe: 1 UP + 1 DOWN,
	// far short of the configured budget of 6 per direction.
	if got := swipeCount(d); got != 2 {
		t.Fatalf("got %d swipes, want 2 (saturation breaks each direction after its first swipe)", got)
	}
}

func TestScrollController_ScrollSearchExhaustionConsumesFullBudgetWhenTreeKeepsChanging(t *testing.T) {
	d := mock.New()
	const budget = 6
	// initial find (1) + scrollSearch pre-loop (1) + budget UP swipes'
	// hierarchy calls + budget DOWN swipes' hierarchy calls, every one
	// distinct so saturation never fires and the full budget runs.
	for i := 0; i < 2+2*budget; i++ {
		d.StageHierarchy(fatTreeWithChangingLeaf(i))
	}
	s, _ := newTestScrollController(d, ScrollControllerOptions{
		MaxIterations:      8,
		MaxScrollFraction:  0.6,
		SwipeDurationMs:    200,
		SettleWaitMs:       500,
		ScrollSearchBudget: budget,
		ViewportInsets:     DefaultInsets,
	})

	_, err := s.EnsureVisible(context.Background(), CompileSelector(Selector{ID: lit("target")}))
	var unreachable *ScrollUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("got error %v (%T), want *ScrollUnreachableError", err, err)
	}
	if unreachable.Message == "" {
		t.Fatal("expected a non-empty message")
	}

	if got := swipeCount(d); got != 2*budget {
		t.Fatalf("got %d swipes, want %d (full UP + DOWN budget exhausted, no saturation)", got, 2*budget)
	}
}

func TestScrollController_BoundsUnchangedAfterScrollReturnsUnreachable(t *testing.T) {
	d := mock.New()
	// The target sits outside the inset viewport (930+50 > 932-50)
	// before AND after the swipe: the positional loop's progress
	// check must fire on the second iteration rather than looping
	// forever.
	offViewport := node("root", "", "0,0,430,932", node("target", "Target", "0,900,430,940"))
	d.StageHierarchy(offViewport)
	d.StageHierarchy(offViewport)
	s, _ := newTestScrollController(d, DefaultScrollControllerOptions())

	_, err := s.EnsureVisible(context.Background(), CompileSelector(Selector{ID: lit("target")}))
	var unreachable *ScrollUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("got error %v (%T), want *ScrollUnreachableError", err, err)
	}
	if unreachable.FinalBounds == nil {
		t.Fatal("expected FinalBounds to be populated with the element's last known position")
	}

	if got := swipeCount(d); got != 1 {
		t.Fatalf("got %d swipes, want exactly 1 (the progress check must fire before a second swipe)", got)
	}
}
