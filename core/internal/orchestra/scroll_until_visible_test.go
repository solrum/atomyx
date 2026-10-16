package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

// busyTree returns a five-leaf screen so the scroll controller does not
// treat it as a thin tree, with an optional extra leaf appended.
func busyTree(extra ...wire.TreeNodeWire) wire.TreeNodeWire {
	children := []wire.TreeNodeWire{
		node("a", "Row A", "0,100,430,140"),
		node("b", "Row B", "0,200,430,240"),
		node("c", "Row C", "0,300,430,340"),
		node("d", "Row D", "0,400,430,440"),
		node("e", "Row E", "0,500,430,540"),
	}
	return node("root", "", "0,0,430,932", append(children, extra...)...)
}

func firstSwipeIsDownward(t *testing.T, d *mock.Driver) bool {
	t.Helper()
	for _, c := range d.Calls {
		if c.Method == "Swipe" {
			from, to := c.Args[0].(wire.Point), c.Args[1].(wire.Point)
			return to.Y < from.Y
		}
	}
	t.Fatalf("no swipe was dispatched")
	return false
}

func TestScrollUntilVisible_AlreadyVisibleDispatchesNoSwipe(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(busyTree(node("target", "Target", "0,600,430,640")))
	o, _ := newTestOrchestra(d)

	result, err := o.ScrollUntilVisible(context.Background(), Selector{ID: lit("target")}, ScrollUntilVisibleOptions{})
	if err != nil {
		t.Fatalf("ScrollUntilVisible: %v", err)
	}
	if !result.Ok || result.ResolvedBy != "id" {
		t.Fatalf("got %+v, want a success resolved by id", result)
	}
	if n := swipeCount(d); n != 0 {
		t.Errorf("got %d swipes, want 0", n)
	}
}

func TestScrollUntilVisible_ScrollsUntilTheElementAppears(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(busyTree())
	d.StageHierarchy(busyTree())
	d.StageHierarchy(busyTree(node("target", "Target", "0,600,430,640")))
	o, _ := newTestOrchestra(d)

	result, err := o.ScrollUntilVisible(context.Background(), Selector{ID: lit("target")}, ScrollUntilVisibleOptions{Direction: ScrollDirectionDown})
	if err != nil {
		t.Fatalf("ScrollUntilVisible: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got %+v, want a success", result)
	}
	if n := swipeCount(d); n != 1 {
		t.Errorf("got %d swipes, want 1", n)
	}
}

func TestScrollUntilVisible_DirectionChoosesTheFirstSearchDirection(t *testing.T) {
	cases := []struct {
		direction    string
		wantDownward bool
	}{
		{ScrollDirectionDown, true},
		{ScrollDirectionUp, false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run("direction="+tc.direction, func(t *testing.T) {
			d := mock.New()
			d.StageHierarchy(busyTree())
			d.StageHierarchy(busyTree())
			d.StageHierarchy(busyTree(node("target", "Target", "0,600,430,640")))
			o, _ := newTestOrchestra(d)

			result, err := o.ScrollUntilVisible(context.Background(), Selector{ID: lit("target")}, ScrollUntilVisibleOptions{Direction: tc.direction})
			if err != nil || !result.Ok {
				t.Fatalf("got result %+v, err %v, want a success", result, err)
			}
			if got := firstSwipeIsDownward(t, d); got != tc.wantDownward {
				t.Errorf("first swipe downward = %t, want %t", got, tc.wantDownward)
			}
		})
	}
}

func TestScrollUntilVisible_MissingElementFailsWithNotFoundCode(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(busyTree())
	o, _ := newTestOrchestra(d)

	result, err := o.ScrollUntilVisible(context.Background(), Selector{ID: lit("ghost")}, ScrollUntilVisibleOptions{Direction: ScrollDirectionDown})
	if err != nil {
		t.Fatalf("ScrollUntilVisible: %v", err)
	}
	if result.Ok {
		t.Fatalf("got a success for an element that never appears")
	}
	if result.Code != diag.CodeElementNotFound {
		t.Errorf("code = %q, want %q", result.Code, diag.CodeElementNotFound)
	}
}

func TestScrollUntilVisible_RejectsBadArguments(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(busyTree())
	o, _ := newTestOrchestra(d)

	_, err := o.ScrollUntilVisible(context.Background(), Selector{ID: lit("x")}, ScrollUntilVisibleOptions{Direction: "sideways"})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Errorf("direction error code = %q (%v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if _, err := o.ScrollUntilVisible(context.Background(), Selector{}, ScrollUntilVisibleOptions{}); err == nil {
		t.Errorf("an empty selector must be rejected")
	}
	if n := swipeCount(d); n != 0 {
		t.Errorf("got %d swipes for rejected calls, want 0", n)
	}
}
