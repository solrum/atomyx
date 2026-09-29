package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

func floatPtr(f float64) *float64 { return &f }

func pointAction(actionType string, x, y float64) script.PointerAction {
	return script.PointerAction{Type: actionType, Target: &script.PointerTarget{Point: &script.Point{X: x, Y: y}}}
}

// singleTouch wraps one action sequence as a one-entry Touches list,
// the single-pointer form of a pointer step.
func singleTouch(actions ...script.PointerAction) []script.PointerTouch {
	return []script.PointerTouch{{Actions: actions}}
}

func newPointerTestContext(tr *testRunner) *CommandContext {
	return &CommandContext{
		Context:   context.Background(),
		Orchestra: tr.orch,
		Clock:     tr.clock,
	}
}

func TestPointerCommand_TapPattern(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 10, 20),
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "pointer tap" {
		t.Fatalf("got detail %q, want \"pointer tap\"", got)
	}
	found := false
	for _, c := range tr.driver.Calls {
		if c.Method == "Tap" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Tap was not dispatched: %+v", tr.driver.Calls)
	}
}

func TestPointerCommand_LongPressPattern(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 10, 20),
			script.PointerAction{Type: "wait", Ms: floatPtr(800)},
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	found := false
	for _, c := range tr.driver.Calls {
		if c.Method == "DispatchGesture" {
			t.Fatalf("a pure long-press (no move) dispatched DispatchGesture, want only LongPress: %+v", tr.driver.Calls)
		}
		if c.Method == "LongPress" {
			if c.Args[1] != 800 {
				t.Fatalf("got LongPress duration %v, want 800", c.Args[1])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("LongPress was not dispatched: %+v", tr.driver.Calls)
	}
}

func TestPointerCommand_DragPattern(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 0, 0),
			pointAction("move", 100, 100),
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	found := false
	for _, c := range tr.driver.Calls {
		if c.Method == "Swipe" {
			found = true
		}
		if c.Method == "DispatchGesture" {
			t.Fatalf("a no-wait drag dispatched DispatchGesture, want only Swipe: %+v", tr.driver.Calls)
		}
	}
	if !found {
		t.Fatalf("Swipe was not dispatched: %+v", tr.driver.Calls)
	}
}

// TestCompilePointerWaypoints_WaitMaterializesHoldSegment proves a
// `wait` between `down` and `move` compiles to an explicit
// same-coordinate waypoint pair (the hold), followed by the drag to
// the move's target, instead of a bare clock advance with no
// waypoint of its own.
func TestCompilePointerWaypoints_WaitMaterializesHoldSegment(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	actions := []script.PointerAction{
		pointAction("down", 10, 20),
		{Type: "wait", Ms: floatPtr(800)},
		pointAction("move", 100, 200),
		{Type: "up"},
	}

	waypoints, err := compilePointerWaypoints(actions, pointerDefaultMoveDurationMs, ctx)
	if err != nil {
		t.Fatalf("compilePointerWaypoints: %v", err)
	}

	want := []driver.GestureWaypoint{
		{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 10, Y: 20}, AtOffsetSeconds: 0},
		{Phase: driver.GesturePhaseMove, Point: wire.Point{X: 10, Y: 20}, AtOffsetSeconds: 0.8},
		{Phase: driver.GesturePhaseMove, Point: wire.Point{X: 100, Y: 200}, AtOffsetSeconds: 1.0},
		{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 100, Y: 200}, AtOffsetSeconds: 1.0},
	}
	if diff := cmp.Diff(want, waypoints); diff != "" {
		t.Fatalf("compiled waypoints mismatch (-want +got):\n%s", diff)
	}

	// waypoints[0] and waypoints[1] share Point (10, 20) at two
	// distinct offsets: that same-coordinate pair is what a consumer
	// realizes as a stationary hold rather than motion.
	if waypoints[0].Point != waypoints[1].Point {
		t.Fatalf("hold segment does not share a coordinate: %+v vs %+v", waypoints[0], waypoints[1])
	}
	if waypoints[1].AtOffsetSeconds == waypoints[0].AtOffsetSeconds {
		t.Fatalf("hold segment has no time separation: both at %v", waypoints[0].AtOffsetSeconds)
	}
}

// TestCompilePointerWaypoints_ConsecutiveWaitsChainHoldSegments proves
// two adjacent `wait` actions each materialize their own same-point
// waypoint, so a multi-wait hold stays stationary throughout rather
// than only across the first wait.
func TestCompilePointerWaypoints_ConsecutiveWaitsChainHoldSegments(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	actions := []script.PointerAction{
		pointAction("down", 5, 5),
		{Type: "wait", Ms: floatPtr(300)},
		{Type: "wait", Ms: floatPtr(200)},
		{Type: "up"},
	}

	waypoints, err := compilePointerWaypoints(actions, pointerDefaultMoveDurationMs, ctx)
	if err != nil {
		t.Fatalf("compilePointerWaypoints: %v", err)
	}

	want := []driver.GestureWaypoint{
		{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 5, Y: 5}, AtOffsetSeconds: 0},
		{Phase: driver.GesturePhaseMove, Point: wire.Point{X: 5, Y: 5}, AtOffsetSeconds: 0.3},
		{Phase: driver.GesturePhaseMove, Point: wire.Point{X: 5, Y: 5}, AtOffsetSeconds: 0.5},
		{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 5, Y: 5}, AtOffsetSeconds: 0.5},
	}
	if diff := cmp.Diff(want, waypoints); diff != "" {
		t.Fatalf("compiled waypoints mismatch (-want +got):\n%s", diff)
	}
}

// TestPointerCommand_HoldThenDragDispatchesGesture proves a
// `down, wait, move, up` sequence (a hold followed by a drag)
// dispatches through DispatchGesture, carrying the compiled hold
// waypoint, instead of through the lossy SwipeAt primitive.
func TestPointerCommand_HoldThenDragDispatchesGesture(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 10, 20),
			script.PointerAction{Type: "wait", Ms: floatPtr(800)},
			pointAction("move", 100, 200),
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "pointer gesture (single pointer, hold-then-drag)" {
		t.Fatalf("got detail %q, want \"pointer gesture (single pointer, hold-then-drag)\"", got)
	}

	var gesture *driver.Gesture
	for _, c := range tr.driver.Calls {
		if c.Method == "Swipe" {
			t.Fatalf("a hold-then-drag sequence dispatched Swipe, want only DispatchGesture: %+v", tr.driver.Calls)
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
		t.Fatalf("got %d waypoints, want 4: %+v", len(waypoints), waypoints)
	}
	if waypoints[0].Point != waypoints[1].Point {
		t.Fatalf("dispatched gesture has no hold segment: %+v", waypoints)
	}
	if waypoints[1].Point == waypoints[2].Point {
		t.Fatalf("dispatched gesture never moves to the drag target: %+v", waypoints)
	}
}

func TestPointerCommand_EmptySequence(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{Command: script.CommandPointer, Touches: singleTouch()}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "POINTER_EMPTY_SEQUENCE: actions list is empty"
	if got := detail(StepResult{Detail: result.Detail}); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestPointerCommand_NoOpeningDown(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(script.PointerAction{Type: "up"}),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "POINTER_NO_OPENING_DOWN: first action must be `down`, got `up`"
	if got := detail(StepResult{Detail: result.Detail}); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

// TestPointerCommand_NoTouches proves a pointer step with an empty
// touches list is rejected with the empty-sequence diagnostic.
func TestPointerCommand_NoTouches(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{Command: script.CommandPointer, Touches: nil}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	if got := detail(StepResult{Detail: result.Detail}); !strings.HasPrefix(got, "POINTER_EMPTY_SEQUENCE") {
		t.Fatalf("got detail %q, want it to start with POINTER_EMPTY_SEQUENCE", got)
	}
}

func TestPointerCommand_MultiPointerRejectedWithoutCapability(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: []script.PointerTouch{
			{ID: "p1", Actions: []script.PointerAction{pointAction("down", 0, 0), {Type: "up"}}},
			{ID: "p2", Actions: []script.PointerAction{pointAction("down", 10, 10), {Type: "up"}}},
		},
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	got := detail(StepResult{Detail: result.Detail})
	if got == "" || got[:len("POINTER_MULTI_NOT_SUPPORTED")] != "POINTER_MULTI_NOT_SUPPORTED" {
		t.Fatalf("got detail %q, want it to start with POINTER_MULTI_NOT_SUPPORTED", got)
	}
	// A cross-platform error surface must not name a platform the caller did
	// not ask about; the guidance stays in terms of what the caller controls.
	lower := strings.ToLower(got)
	if strings.Contains(lower, "ios") || strings.Contains(lower, "android") {
		t.Fatalf("error message names a platform, want platform-neutral guidance: %q", got)
	}
}

func TestPointerCommand_MultiPointerDispatchesGesture(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: []script.PointerTouch{
			{ID: "p1", Actions: []script.PointerAction{pointAction("down", 0, 0), {Type: "up"}}},
			{ID: "p2", Actions: []script.PointerAction{pointAction("down", 10, 10), {Type: "up"}}},
		},
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	if got := detail(StepResult{Detail: result.Detail}); got != "pointer 2-pointer gesture" {
		t.Fatalf("got detail %q", got)
	}
}

func TestPointerCommand_CaptureFirstRejected(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			script.PointerAction{Type: "capture"},
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "POINTER_NO_OPENING_DOWN: first action must be `down`, got `capture`"
	if got := detail(StepResult{Detail: result.Detail}); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestPointerCommand_CaptureLastRejected(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 0, 0),
			script.PointerAction{Type: "capture"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "POINTER_NO_CLOSING_UP: last action must be `up`, got `capture`"
	if got := detail(StepResult{Detail: result.Detail}); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestPointerCommand_CaptureOutsideDownUpRejected(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			pointAction("down", 0, 0),
			script.PointerAction{Type: "up"},
			script.PointerAction{Type: "capture"},
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "POINTER_NO_OPENING_DOWN: action[2] is `capture` but no pointer is currently down"
	if got := detail(StepResult{Detail: result.Detail}); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestPointerCommand_CaptureDispatchesGestureAndRoutesArtifact(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 1 }\n          - capture\n          - up\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 {
		t.Fatalf("got %d screenshots, want 1: %+v", len(shots), shots)
	}
	if shots[0].Label != "step-1-capture-1" {
		t.Fatalf("got label %q, want \"step-1-capture-1\"", shots[0].Label)
	}
	if len(shots[0].Data) == 0 {
		t.Fatal("captured screenshot data is empty")
	}
	found := false
	for _, c := range tr.driver.Calls {
		if c.Method == "DispatchGesture" {
			found = true
		}
		if c.Method == "Tap" || c.Method == "LongPress" || c.Method == "Swipe" {
			t.Fatalf("a capture-carrying sequence dispatched %s, want only DispatchGesture", c.Method)
		}
	}
	if !found {
		t.Fatalf("DispatchGesture was not dispatched: %+v", tr.driver.Calls)
	}
}

func TestPointerCommand_CaptureLabelOverridesDefault(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 1 }\n          - capture: mid-drag\n          - up\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "mid-drag" {
		t.Fatalf("got screenshots %+v, want [mid-drag]", shots)
	}
}

func TestPointerCommand_MultipleCapturesRouteOrderedArtifacts(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- pointer:\n"+
		"    touches:\n"+
		"      - actions:\n"+
		"          - down: { x: 1, y: 1 }\n"+
		"          - capture\n"+
		"          - move: { x: 2, y: 2 }\n"+
		"          - capture\n"+
		"          - up\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 2 {
		t.Fatalf("got %d screenshots, want 2: %+v", len(shots), shots)
	}
	if shots[0].Label != "step-1-capture-1" || shots[1].Label != "step-1-capture-2" {
		t.Fatalf("got labels [%q, %q], want [\"step-1-capture-1\", \"step-1-capture-2\"]", shots[0].Label, shots[1].Label)
	}
}

func TestPointerCommand_NoCaptureProducesNoArtifact(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	result := run(t, tr, "appId: com.test\n---\n- pointer:\n    touches:\n      - actions:\n          - down: { x: 1, y: 1 }\n          - up\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if shots := result.Artifacts.Screenshots(); len(shots) != 0 {
		t.Fatalf("got screenshots %+v, want none", shots)
	}
}

func TestPointerCommand_MultiPointerCaptureRoutesArtifact(t *testing.T) {
	tr := newTestRunner()
	tr.driver.SetCapabilities(driver.Capabilities{CanMultiPointer: true})
	ctx := newPointerTestContext(tr)
	ctx.Artifacts = newInMemoryArtifacts()
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: []script.PointerTouch{
			{ID: "p1", Actions: []script.PointerAction{pointAction("down", 0, 0), {Type: "capture"}, {Type: "up"}}},
			{ID: "p2", Actions: []script.PointerAction{pointAction("down", 10, 10), {Type: "up"}}},
		},
	}

	result := PointerCommand{}.Execute(step, ctx)
	if !result.Ok {
		t.Fatalf("got Ok=false: %+v", result)
	}
	shots := ctx.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "step-1-capture-1" {
		t.Fatalf("got screenshots %+v, want [step-1-capture-1]", shots)
	}
}

func TestPointerCommand_PressureRejectedWithoutCapability(t *testing.T) {
	tr := newTestRunner()
	ctx := newPointerTestContext(tr)
	step := &script.PointerStep{
		Command: script.CommandPointer,
		Touches: singleTouch(
			script.PointerAction{Type: "down", Target: &script.PointerTarget{Point: &script.Point{X: 0, Y: 0}}, Pressure: floatPtr(0.5)},
			script.PointerAction{Type: "up"},
		),
	}

	result := PointerCommand{}.Execute(step, ctx)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	got := detail(StepResult{Detail: result.Detail})
	want := "POINTER_PRESSURE_NOT_SUPPORTED"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("got detail %q, want it to start with %q", got, want)
	}
	if strings.Contains(strings.ToLower(got), "ios") || strings.Contains(strings.ToLower(got), "android") {
		t.Fatalf("detail %q names a platform; the message must stay platform-neutral", got)
	}
	if !strings.Contains(got, "pressure") {
		t.Fatalf("detail %q lacks concrete guidance about the `pressure` field", got)
	}
}
