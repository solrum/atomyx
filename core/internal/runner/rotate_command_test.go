package runner

import (
	"math"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

func approxPoint(t *testing.T, label string, got, want wire.Point) {
	t.Helper()
	const eps = 1e-6
	if math.Abs(got.X-want.X) > eps || math.Abs(got.Y-want.Y) > eps {
		t.Fatalf("%s: got %+v, want ~%+v", label, got, want)
	}
}

func TestRotateCommand_CoordinateCenterBuildsTwoArcs(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	step := &script.RotateStep{Command: script.CommandRotate, Center: targetPoint(200, 200), Degrees: 90}

	result := RotateCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "rotate 90 degrees" {
		t.Fatalf("got detail %q", got)
	}

	gesture := gestureFromCalls(t, tr.driver.Calls)
	if len(gesture.Pointers) != 2 {
		t.Fatalf("got %d pointers, want 2: %+v", len(gesture.Pointers), gesture)
	}

	// Each contact traces an arc: one down, rotateArcSegments moves,
	// one up. The arc is more than a chord, since it passes through
	// intermediate points off the straight line between endpoints.
	for _, p := range gesture.Pointers {
		if len(p.Waypoints) != rotateArcSegments+2 {
			t.Fatalf("pointer %q: got %d waypoints, want %d (down, %d moves, up)",
				p.ID, len(p.Waypoints), rotateArcSegments+2, rotateArcSegments)
		}
		if p.Waypoints[0].Phase != driver.GesturePhaseDown {
			t.Fatalf("pointer %q does not open with down", p.ID)
		}
		if p.Waypoints[len(p.Waypoints)-1].Phase != driver.GesturePhaseUp {
			t.Fatalf("pointer %q does not close with up", p.ID)
		}
		// Every contact stays on the circle of radius 100 around the center.
		for _, w := range p.Waypoints {
			r := math.Hypot(w.Point.X-200, w.Point.Y-200)
			if math.Abs(r-100) > 1e-6 {
				t.Fatalf("pointer %q waypoint %+v is not on the radius-100 circle (r=%v)", p.ID, w.Point, r)
			}
		}
	}

	// Contacts start on opposite sides (angles 0 and 180) and sweep 90
	// degrees clockwise to (200,300) and (200,100).
	approxPoint(t, "rotate-1 down", gesture.Pointers[0].Waypoints[0].Point, wire.Point{X: 300, Y: 200})
	approxPoint(t, "rotate-1 final", gesture.Pointers[0].Waypoints[rotateArcSegments].Point, wire.Point{X: 200, Y: 300})
	approxPoint(t, "rotate-2 down", gesture.Pointers[1].Waypoints[0].Point, wire.Point{X: 100, Y: 200})
	approxPoint(t, "rotate-2 final", gesture.Pointers[1].Waypoints[rotateArcSegments].Point, wire.Point{X: 200, Y: 100})
}

func TestRotateCommand_CustomRadius(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	step := &script.RotateStep{Command: script.CommandRotate, Center: targetPoint(200, 200), Degrees: -45, Radius: floatPtr(50)}

	result := RotateCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	gesture := gestureFromCalls(t, tr.driver.Calls)
	approxPoint(t, "rotate-1 down", gesture.Pointers[0].Waypoints[0].Point, wire.Point{X: 250, Y: 200})
	approxPoint(t, "rotate-2 down", gesture.Pointers[1].Waypoints[0].Point, wire.Point{X: 150, Y: 200})
}

func TestRotateCommand_SelectorCenterResolvesToElementCenter(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	tr.driver.StageHierarchy(rootTree(button("dial", "Dial", "100,100,300,300")))

	result := run(t, tr, "appId: com.test\n---\n- launchApp\n- rotate:\n    center: { id: dial }\n    degrees: 90\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	gesture := gestureFromCalls(t, tr.driver.Calls)
	if len(gesture.Pointers) != 2 {
		t.Fatalf("got %d pointers, want 2", len(gesture.Pointers))
	}
	// Element bounds 100,100,300,300 center at (200,200), radius default 100.
	approxPoint(t, "rotate-1 down", gesture.Pointers[0].Waypoints[0].Point, wire.Point{X: 300, Y: 200})
	approxPoint(t, "rotate-2 down", gesture.Pointers[1].Waypoints[0].Point, wire.Point{X: 100, Y: 200})
}

func TestRotateCommand_RejectedWithoutMultiPointer(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.RotateStep{Command: script.CommandRotate, Center: targetPoint(200, 200), Degrees: 90}

	result := RotateCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want a multi-pointer capability rejection")
	}
	got := detail(StepResult{Detail: result.Detail})
	if len(got) < len("POINTER_MULTI_NOT_SUPPORTED") || got[:len("POINTER_MULTI_NOT_SUPPORTED")] != "POINTER_MULTI_NOT_SUPPORTED" {
		t.Fatalf("got detail %q, want it to start with POINTER_MULTI_NOT_SUPPORTED", got)
	}
	for _, c := range tr.driver.Calls {
		if c.Method == "DispatchGesture" {
			t.Fatalf("rotate dispatched a gesture despite the missing capability: %+v", tr.driver.Calls)
		}
	}
}
