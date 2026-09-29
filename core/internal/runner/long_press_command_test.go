package runner

import (
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

func targetPoint(x, y float64) *script.PointerTarget {
	return &script.PointerTarget{Point: &script.Point{X: x, Y: y}}
}

func TestLongPressCommand_HoldOnly(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.LongPressStep{Command: script.CommandLongPress, At: targetPoint(30, 40), HoldMs: floatPtr(650)}

	result := LongPressCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "longPress (hold 650ms)" {
		t.Fatalf("got detail %q, want \"longPress (hold 650ms)\"", got)
	}
	var found bool
	for _, c := range tr.driver.Calls {
		if c.Method == "DispatchGesture" {
			t.Fatalf("a hold-only longPress dispatched DispatchGesture, want LongPress: %+v", tr.driver.Calls)
		}
		if c.Method == "LongPress" {
			if c.Args[1] != 650 {
				t.Fatalf("got LongPress duration %v, want 650", c.Args[1])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("LongPress was not dispatched: %+v", tr.driver.Calls)
	}
}

func TestLongPressCommand_DefaultHold(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.LongPressStep{Command: script.CommandLongPress, At: targetPoint(10, 10)}

	result := LongPressCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "longPress (hold 500ms)" {
		t.Fatalf("got detail %q, want the 500 ms default hold", got)
	}
	for _, c := range tr.driver.Calls {
		if c.Method == "LongPress" && c.Args[1] != 500 {
			t.Fatalf("got LongPress duration %v, want the 500 ms default", c.Args[1])
		}
	}
}

func TestLongPressCommand_ThenSwipeToDispatchesHoldThenDrag(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.LongPressStep{
		Command:     script.CommandLongPress,
		At:          targetPoint(10, 20),
		HoldMs:      floatPtr(600),
		ThenSwipeTo: targetPoint(100, 200),
	}

	result := LongPressCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "longPress (hold 600ms, then drag)" {
		t.Fatalf("got detail %q", got)
	}

	var gesture *driver.Gesture
	for _, c := range tr.driver.Calls {
		if c.Method == "Swipe" {
			t.Fatalf("a hold-then-drag longPress dispatched Swipe, want DispatchGesture: %+v", tr.driver.Calls)
		}
		if c.Method == "DispatchGesture" {
			g := c.Args[0].(driver.Gesture)
			gesture = &g
		}
	}
	if gesture == nil {
		t.Fatalf("DispatchGesture was not dispatched: %+v", tr.driver.Calls)
	}
	waypoints := gesture.Pointers[0].Waypoints
	if len(waypoints) != 4 {
		t.Fatalf("got %d waypoints, want 4 (down, hold, drag, up): %+v", len(waypoints), waypoints)
	}
	if waypoints[0].Point != (wire.Point{X: 10, Y: 20}) || waypoints[1].Point != (wire.Point{X: 10, Y: 20}) {
		t.Fatalf("hold segment does not stay at the down point: %+v", waypoints)
	}
	if waypoints[2].Point != (wire.Point{X: 100, Y: 200}) {
		t.Fatalf("drag does not reach thenSwipeTo: %+v", waypoints)
	}
}
