package runner

import (
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

// gestureFromCalls returns the single gesture a run dispatched through
// DispatchGesture, failing when none or more than one was dispatched.
func gestureFromCalls(t *testing.T, calls []mock.Call) driver.Gesture {
	t.Helper()
	var found []driver.Gesture
	for _, c := range calls {
		if c.Method == "DispatchGesture" {
			found = append(found, c.Args[0].(driver.Gesture))
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d DispatchGesture calls, want 1: %+v", len(found), calls)
	}
	return found[0]
}

func TestPinchCommand_CoordinateCenterBuildsTwoSymmetricTouches(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	step := &script.PinchStep{
		Command:  script.CommandPinch,
		Center:   targetPoint(200, 200),
		StartGap: 40,
		EndGap:   120,
	}

	result := PinchCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "pinch vertical (gap 40 to 120)" {
		t.Fatalf("got detail %q", got)
	}

	gesture := gestureFromCalls(t, tr.driver.Calls)
	if len(gesture.Pointers) != 2 {
		t.Fatalf("got %d pointers, want 2: %+v", len(gesture.Pointers), gesture)
	}
	// Vertical pinch about (200,200): one contact above, one below,
	// starting 40 apart (±20) and moving to 120 apart (±60).
	assertDownMove(t, gesture.Pointers[0], wire.Point{X: 200, Y: 220}, wire.Point{X: 200, Y: 260})
	assertDownMove(t, gesture.Pointers[1], wire.Point{X: 200, Y: 180}, wire.Point{X: 200, Y: 140})
}

func TestPinchCommand_HorizontalAxis(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	axis := "horizontal"
	step := &script.PinchStep{
		Command:  script.CommandPinch,
		Center:   targetPoint(200, 200),
		StartGap: 100,
		EndGap:   20,
		Axis:     &axis,
	}

	result := PinchCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	gesture := gestureFromCalls(t, tr.driver.Calls)
	assertDownMove(t, gesture.Pointers[0], wire.Point{X: 250, Y: 200}, wire.Point{X: 210, Y: 200})
	assertDownMove(t, gesture.Pointers[1], wire.Point{X: 150, Y: 200}, wire.Point{X: 190, Y: 200})
}

func TestPinchCommand_SelectorCenterResolvesToElementCenter(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	tr.driver.StageHierarchy(rootTree(button("target", "Target", "100,100,300,300")))

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n- pinch:\n    center: { id: target }\n    startGap: 40\n    endGap: 120\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	gesture := gestureFromCalls(t, tr.driver.Calls)
	if len(gesture.Pointers) != 2 {
		t.Fatalf("got %d pointers, want 2", len(gesture.Pointers))
	}
	// Element bounds 100,100,300,300 center at (200,200).
	assertDownMove(t, gesture.Pointers[0], wire.Point{X: 200, Y: 220}, wire.Point{X: 200, Y: 260})
	assertDownMove(t, gesture.Pointers[1], wire.Point{X: 200, Y: 180}, wire.Point{X: 200, Y: 140})
}

func TestPinchCommand_RejectedWithoutMultiPointer(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PinchStep{Command: script.CommandPinch, Center: targetPoint(200, 200), StartGap: 40, EndGap: 120}

	result := PinchCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want a multi-pointer capability rejection")
	}
	got := detail(StepResult{Detail: result.Detail})
	if len(got) < len("POINTER_MULTI_NOT_SUPPORTED") || got[:len("POINTER_MULTI_NOT_SUPPORTED")] != "POINTER_MULTI_NOT_SUPPORTED" {
		t.Fatalf("got detail %q, want it to start with POINTER_MULTI_NOT_SUPPORTED", got)
	}
	for _, c := range tr.driver.Calls {
		if c.Method == "DispatchGesture" {
			t.Fatalf("pinch dispatched a gesture despite the missing capability: %+v", tr.driver.Calls)
		}
	}
}

// assertDownMove checks a pointer opens with a `down` at wantDown,
// moves to wantMove, and closes with `up` at the move point.
func assertDownMove(t *testing.T, p driver.GesturePointer, wantDown, wantMove wire.Point) {
	t.Helper()
	w := p.Waypoints
	if len(w) != 3 {
		t.Fatalf("pointer %q: got %d waypoints, want 3 (down, move, up): %+v", p.ID, len(w), w)
	}
	if w[0].Phase != driver.GesturePhaseDown || w[0].Point != wantDown {
		t.Fatalf("pointer %q: got down %+v, want %+v", p.ID, w[0], wantDown)
	}
	if w[1].Phase != driver.GesturePhaseMove || w[1].Point != wantMove {
		t.Fatalf("pointer %q: got move %+v, want %+v", p.ID, w[1], wantMove)
	}
	if w[2].Phase != driver.GesturePhaseUp {
		t.Fatalf("pointer %q: does not close with up: %+v", p.ID, w)
	}
}
