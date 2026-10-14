package runner

import (
	"errors"
	"fmt"
	"strings"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/wire"
)

const (
	// pointerMaxWaitMs caps a `wait: <ms>` action so a typo cannot
	// stall a script for minutes silently.
	pointerMaxWaitMs = 30_000
	// pointerMaxMoveDurationMs caps moveDurationMs for the same reason.
	pointerMaxMoveDurationMs = 10_000
	// pointerDefaultMoveDurationMs is the drag duration used when the
	// step carries no moveDurationMs, matching SwipeAt's own default.
	pointerDefaultMoveDurationMs = 200
	// pointerDragMinPressMs is the minimum press-hold XCUITest
	// reliably accepts as "press then drag" for a naked
	// [down, move, up] sequence with no explicit wait.
	pointerDragMinPressMs = 50
)

// pointerError is the structured failure pointer's own validators and
// target resolution raise. Its Error() rendering, "<code>: <detail>",
// becomes the step's failed CommandResult detail. Any other error
// bubbling out of executePointer signals a failure outside the
// pointer command's own validation, so it propagates as a panic
// instead: the runner's per-step dispatch recovers it and reports it
// as an "Exception: " prefixed detail.
type pointerError struct {
	code   string
	detail string
}

func (e *pointerError) Error() string { return e.code + ": " + e.detail }

func newPointerError(code, detail string) *pointerError {
	return &pointerError{code: code, detail: detail}
}

// PointerCommand dispatches a W3C Actions pointer gesture from the
// step's Touches list: a single touch is a single-pointer gesture,
// more than one touch is a multi-pointer gesture whose paths run in
// parallel.
type PointerCommand struct{}

func (PointerCommand) Command() string { return script.CommandPointer }

func (PointerCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.PointerStep)

	result, err := executePointer(s, ctx)
	if err != nil {
		var perr *pointerError
		if errors.As(err, &perr) {
			return CommandResult{Ok: false, Detail: strPtr(perr.Error())}
		}
		panic(err)
	}
	return result
}

func executePointer(s *script.PointerStep, ctx *CommandContext) (CommandResult, error) {
	moveDurationMs, err := validatePointerMoveDuration(s.MoveDurationMs)
	if err != nil {
		return CommandResult{}, err
	}

	if len(s.Touches) == 0 {
		return CommandResult{}, newPointerError("POINTER_EMPTY_SEQUENCE",
			"pointer: `touches` must have at least one entry")
	}

	if len(s.Touches) > 1 {
		pointerCount, err := dispatchMultiTouch(s.Touches, moveDurationMs, ctx)
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("pointer %d-pointer gesture", pointerCount))}, nil
	}

	reason, err := dispatchSingleTouch(s.Touches[0].Actions, moveDurationMs, ctx)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Ok: true, Detail: strPtr("pointer " + reason)}, nil
}

// dispatchSingleTouch validates one pointer path and dispatches it
// exactly as a lone pointer touch does: through the fast-path
// classification into a tap, longPress, or drag primitive, or through
// the waypoint gesture when a trait (pressure, capture, or a
// hold-then-drag) forces it. It returns a short reason naming which
// path fired, for the caller's own result detail: the primitive's
// kind for the fast path, or "gesture (single pointer, <trait>)" for
// the waypoint path. Every gesture-sugar step whose gesture reduces to
// a single pointer routes through here, so the raw pointer step and
// the sugar steps share one dispatch and one capture-routing path.
func dispatchSingleTouch(actions []script.PointerAction, moveDurationMs float64, ctx *CommandContext) (string, error) {
	if err := validateSingleSequence(actions); err != nil {
		return "", err
	}
	if err := validatePressureCapability(actions, ctx); err != nil {
		return "", err
	}

	if hasPressure(actions) || hasCapture(actions) || hasHoldThenDrag(actions) {
		waypoints, err := compilePointerWaypoints(actions, moveDurationMs, ctx)
		if err != nil {
			return "", err
		}
		gesture := driver.Gesture{Pointers: []driver.GesturePointer{{ID: "single", Waypoints: waypoints}}}
		result := must(ctx.Orchestra.DispatchGesture(ctx.Context, gesture))
		routePointerCaptures(result.CapturedImages, pointerCaptureLabels(actions, ctx.StepIndex), ctx)
		return fmt.Sprintf("gesture (single pointer, %s)", singlePointerGestureReason(actions)), nil
	}

	pattern, err := classifyPointerPattern(actions, ctx)
	if err != nil {
		return "", err
	}
	if err := dispatchPointerPattern(pattern, ctx); err != nil {
		return "", err
	}
	return pattern.kind, nil
}

// dispatchMultiTouch validates a multi-pointer gesture (checking the
// multi-pointer capability gate and per-touch shape), compiles its
// touches to a parallel-path gesture, dispatches it, and routes any
// mid-gesture captures. It returns the number of pointers dispatched,
// for the caller's own result detail. Every gesture-sugar step that
// builds more than one touch (pinch, rotate) routes through here, so
// the capability gate and capture routing stay in one place.
func dispatchMultiTouch(touches []script.PointerTouch, moveDurationMs float64, ctx *CommandContext) (int, error) {
	if err := validateMultiPointer(touches, ctx); err != nil {
		return 0, err
	}
	var allActions []script.PointerAction
	for _, t := range touches {
		allActions = append(allActions, t.Actions...)
	}
	if err := validatePressureCapability(allActions, ctx); err != nil {
		return 0, err
	}
	gesture, err := compileMultiPointer(touches, moveDurationMs, ctx)
	if err != nil {
		return 0, err
	}
	result := must(ctx.Orchestra.DispatchGesture(ctx.Context, gesture))
	routePointerCaptures(result.CapturedImages, pointerCaptureLabels(allActions, ctx.StepIndex), ctx)
	return len(gesture.Pointers), nil
}

func hasPressure(actions []script.PointerAction) bool {
	for _, a := range actions {
		if (a.Type == "down" || a.Type == "move") && a.Pressure != nil {
			return true
		}
	}
	return false
}

// hasCapture reports whether actions carries a mid-gesture screenshot
// marker. A sequence carrying one always dispatches through the
// waypoint-based gesture path, the same way a pressure-sensitive
// sequence does, because the coordinate primitives (Tap, LongPress,
// Swipe) have no way to signal a capture point.
func hasCapture(actions []script.PointerAction) bool {
	for _, a := range actions {
		if a.Type == "capture" {
			return true
		}
	}
	return false
}

// hasHoldThenDrag reports whether actions carries a nonzero `wait`
// followed later by a `move`: a stationary hold followed by a drag.
// The coordinate primitive that would otherwise dispatch this shape,
// SwipeAt, has no parameter for a hold duration separate from its
// drag duration, so it collapses the wait into extra travel time
// instead of a stationary pause. Routing this shape through the
// waypoint-based gesture path keeps the hold and the drag distinct,
// the same way a pressure-sensitive or capture-carrying sequence
// already does.
func hasHoldThenDrag(actions []script.PointerAction) bool {
	seenNonZeroWait := false
	for _, a := range actions {
		if a.Type == "wait" {
			ms := 0.0
			if a.Ms != nil {
				ms = *a.Ms
			}
			if ms > 0 {
				seenNonZeroWait = true
			}
			continue
		}
		if a.Type == "move" && seenNonZeroWait {
			return true
		}
	}
	return false
}

func validatePointerMoveDuration(raw *float64) (float64, error) {
	if raw == nil {
		return pointerDefaultMoveDurationMs, nil
	}
	if *raw < 0 || *raw > pointerMaxMoveDurationMs {
		return 0, newPointerError("POINTER_INVALID_MOVE_DURATION",
			fmt.Sprintf("moveDurationMs must be between 0 and %d, got %v", pointerMaxMoveDurationMs, *raw))
	}
	return *raw, nil
}

func validatePressureCapability(actions []script.PointerAction, ctx *CommandContext) error {
	if !hasPressure(actions) {
		return nil
	}
	if !ctx.Orchestra.Capabilities().CanPressure {
		return newPointerError("POINTER_PRESSURE_NOT_SUPPORTED",
			"pressure-sensitive waypoints are not supported by the active driver. "+
				"Remove the `pressure` field from every `down` or `move` action, or "+
				"run the script against a driver whose capabilities report pressure-"+
				"sensitive dispatch support.")
	}
	return nil
}

func validateMultiPointer(touches []script.PointerTouch, ctx *CommandContext) error {
	if !ctx.Orchestra.Capabilities().CanMultiPointer {
		return newPointerError("POINTER_MULTI_NOT_SUPPORTED",
			"multi-pointer gestures are not supported by the active driver. "+
				"Rewrite the gesture to use a single pointer, or run the script "+
				"against a driver whose capabilities report multi-pointer support.")
	}
	seen := make(map[string]struct{}, len(touches))
	for _, t := range touches {
		if _, dup := seen[t.ID]; dup {
			return newPointerError("POINTER_FORM_CONFLICT",
				fmt.Sprintf("duplicate pointer id %q. Each pointer in a multi-pointer gesture needs a unique id.", t.ID))
		}
		seen[t.ID] = struct{}{}
	}
	for _, t := range touches {
		if err := validateSingleSequence(t.Actions); err != nil {
			var perr *pointerError
			if errors.As(err, &perr) {
				return newPointerError(perr.code, fmt.Sprintf("pointer %q: %s", t.ID, perr.detail))
			}
			return err
		}
	}
	return nil
}

// validateSingleSequence checks that actions opens with `down`,
// closes with `up`, never nests a `down` inside another open `down`,
// and keeps every `wait` in range. This is the shape every single-pointer
// sequence (and each pointer within a multi-pointer gesture) must
// have before it can be classified or compiled into waypoints.
func validateSingleSequence(actions []script.PointerAction) error {
	if len(actions) == 0 {
		return newPointerError("POINTER_EMPTY_SEQUENCE", "actions list is empty")
	}
	if actions[0].Type != "down" {
		return newPointerError("POINTER_NO_OPENING_DOWN",
			fmt.Sprintf("first action must be `down`, got `%s`", actions[0].Type))
	}
	if actions[len(actions)-1].Type != "up" {
		return newPointerError("POINTER_NO_CLOSING_UP",
			fmt.Sprintf("last action must be `up`, got `%s`", actions[len(actions)-1].Type))
	}

	openDown := false
	for i, a := range actions {
		switch a.Type {
		case "down":
			if openDown {
				return newPointerError("POINTER_NESTED_DOWN", fmt.Sprintf(
					"action[%d] is `down` but the previous `down` has not yet been closed by `up`", i))
			}
			openDown = true
		case "up":
			if !openDown {
				return newPointerError("POINTER_NO_OPENING_DOWN", fmt.Sprintf(
					"action[%d] is `up` without a matching preceding `down`", i))
			}
			openDown = false
		case "wait":
			if !openDown {
				return newPointerError("POINTER_NO_OPENING_DOWN", fmt.Sprintf(
					"action[%d] is `wait` but no pointer is currently down", i))
			}
			ms := 0.0
			if a.Ms != nil {
				ms = *a.Ms
			}
			if ms < 0 || ms > pointerMaxWaitMs {
				return newPointerError("POINTER_INVALID_WAIT", fmt.Sprintf(
					"action[%d] wait(%v) out of range [0..%d]", i, ms, pointerMaxWaitMs))
			}
		case "capture":
			if !openDown {
				return newPointerError("POINTER_NO_OPENING_DOWN", fmt.Sprintf(
					"action[%d] is `capture` but no pointer is currently down", i))
			}
		default: // "move"
			if !openDown {
				return newPointerError("POINTER_NO_OPENING_DOWN", fmt.Sprintf(
					"action[%d] is `move` but no pointer is currently down", i))
			}
		}
	}
	return nil
}

// compilePointerWaypoints walks a validated action list, accumulating
// a clock that advances by each action's explicit wait or by
// moveDurationMs on a move; down, up, and capture are zero-cost.
//
// A `wait` also appends a waypoint at the live point (the point of the
// most recent down or move), holding the same coordinate across the
// pre-wait and post-wait offsets. A bare clock advance with no
// matching waypoint is indistinguishable from motion to at least one
// platform's gesture consumer, so a hold must be represented as a
// same-coordinate segment in the waypoint data itself, not as a gap
// between two differently-timed waypoints.
func compilePointerWaypoints(
	actions []script.PointerAction, moveDurationMs float64, ctx *CommandContext,
) ([]driver.GestureWaypoint, error) {
	var waypoints []driver.GestureWaypoint
	clockSeconds := 0.0
	var lastPoint *wire.Point

	for _, action := range actions {
		switch action.Type {
		case "down":
			point, err := resolvePointerTarget(action.Target, ctx)
			if err != nil {
				return nil, err
			}
			waypoints = append(waypoints, driver.GestureWaypoint{
				Phase: driver.GesturePhaseDown, Point: point, AtOffsetSeconds: clockSeconds, Pressure: action.Pressure,
			})
			lastPoint = &point
		case "move":
			point, err := resolvePointerTarget(action.Target, ctx)
			if err != nil {
				return nil, err
			}
			clockSeconds += moveDurationMs / 1000
			waypoints = append(waypoints, driver.GestureWaypoint{
				Phase: driver.GesturePhaseMove, Point: point, AtOffsetSeconds: clockSeconds, Pressure: action.Pressure,
			})
			lastPoint = &point
		case "wait":
			ms := 0.0
			if action.Ms != nil {
				ms = *action.Ms
			}
			clockSeconds += ms / 1000
			if lastPoint != nil {
				waypoints = append(waypoints, driver.GestureWaypoint{
					Phase: driver.GesturePhaseMove, Point: *lastPoint, AtOffsetSeconds: clockSeconds,
				})
			}
		case "capture":
			point := wire.Point{}
			if lastPoint != nil {
				point = *lastPoint
			}
			waypoints = append(waypoints, driver.GestureWaypoint{
				Phase: driver.GesturePhaseCapture, Point: point, AtOffsetSeconds: clockSeconds,
			})
		default: // "up"
			point := wire.Point{}
			if lastPoint != nil {
				point = *lastPoint
			}
			waypoints = append(waypoints, driver.GestureWaypoint{
				Phase: driver.GesturePhaseUp, Point: point, AtOffsetSeconds: clockSeconds,
			})
		}
	}
	return waypoints, nil
}

// singlePointerGestureReason renders which trait forced the
// single-pointer sequence through the waypoint-based gesture path,
// for the step's own result detail.
func singlePointerGestureReason(actions []script.PointerAction) string {
	var reasons []string
	if hasPressure(actions) {
		reasons = append(reasons, "pressure")
	}
	if hasCapture(actions) {
		reasons = append(reasons, "capture")
	}
	if hasHoldThenDrag(actions) {
		reasons = append(reasons, "hold-then-drag")
	}
	return strings.Join(reasons, "+")
}

// pointerCaptureLabels returns, in the order they appear across
// actions, the artifact label each `capture` action produces: the
// action's own Label when set, or a "step-N-capture-M" default
// matching ScreenshotCommand's own "step-N" convention, M being the
// capture's 1-indexed ordinal within this step.
func pointerCaptureLabels(actions []script.PointerAction, stepIndex int) []string {
	var labels []string
	n := 0
	for _, a := range actions {
		if a.Type != "capture" {
			continue
		}
		n++
		if a.Label != nil {
			labels = append(labels, *a.Label)
			continue
		}
		labels = append(labels, fmt.Sprintf("step-%d-capture-%d", stepIndex+1, n))
	}
	return labels
}

// routePointerCaptures stores every image a dispatched gesture
// captured into the run's artifacts, through the same
// ScriptArtifacts sink ScreenshotCommand uses, so a pointer step that
// captured mid-gesture produces report artifacts exactly as a
// screenshot step does. images and labels are aligned by position;
// an image past the end of labels (never expected, since the two are
// always built from the same capture markers) falls back to a plain
// ordinal label rather than dropping the image.
func routePointerCaptures(images []driver.CapturedImage, labels []string, ctx *CommandContext) {
	for i, img := range images {
		label := fmt.Sprintf("capture-%d", i+1)
		if i < len(labels) {
			label = labels[i]
		}
		ctx.Artifacts.AddScreenshot(ctx.artifactLabel(label), img.Data, img.MimeType)
	}
}

func compileMultiPointer(
	touches []script.PointerTouch, moveDurationMs float64, ctx *CommandContext,
) (driver.Gesture, error) {
	compiled := make([]driver.GesturePointer, 0, len(touches))
	for _, t := range touches {
		waypoints, err := compilePointerWaypoints(t.Actions, moveDurationMs, ctx)
		if err != nil {
			return driver.Gesture{}, err
		}
		compiled = append(compiled, driver.GesturePointer{ID: t.ID, Waypoints: waypoints})
	}
	return driver.Gesture{Pointers: compiled}, nil
}

// resolvePointerTarget resolves target to an absolute point: a
// coordinate target passes through unchanged; a bare selector target
// (no anchor, no offset) resolves to the element's center; a selector
// with an anchor and/or offset resolves the element's bounds and
// anchors + offsets from there.
func resolvePointerTarget(target *script.PointerTarget, ctx *CommandContext) (wire.Point, error) {
	if target == nil {
		return wire.Point{}, newPointerError("POINTER_SELECTOR_RESOLUTION_FAILED", "pointer target is missing")
	}
	if target.Point != nil {
		return wire.Point{X: target.Point.X, Y: target.Point.Y}, nil
	}
	if target.Selector == nil {
		return wire.Point{}, newPointerError("POINTER_SELECTOR_RESOLUTION_FAILED",
			"pointer target has neither a point nor a selector")
	}

	selector := compileScriptSelector(*target.Selector)

	if target.Anchor == nil && target.Offset == nil {
		point, err := ctx.Orchestra.ResolvePoint(ctx.Context, selector)
		if err != nil {
			return wire.Point{}, newPointerError("POINTER_SELECTOR_RESOLUTION_FAILED", err.Error())
		}
		return point, nil
	}

	bounds, err := ctx.Orchestra.ResolveBounds(ctx.Context, selector)
	if err != nil {
		return wire.Point{}, newPointerError("POINTER_SELECTOR_RESOLUTION_FAILED", err.Error())
	}
	anchorName := "center"
	if target.Anchor != nil {
		anchorName = *target.Anchor
	}
	anchorPoint, err := orchestra.BoundsAnchor(bounds, anchorName)
	if err != nil {
		return wire.Point{}, newPointerError("POINTER_INVALID_ANCHOR", err.Error())
	}
	dx, dy := 0.0, 0.0
	if target.Offset != nil {
		if target.Offset.Dx != nil {
			dx = *target.Offset.Dx
		}
		if target.Offset.Dy != nil {
			dy = *target.Offset.Dy
		}
	}
	return wire.Point{X: anchorPoint.X + dx, Y: anchorPoint.Y + dy}, nil
}

// pointerPattern is a validated single-pointer sequence reduced to
// one of the four shapes the coordinate primitives can dispatch.
type pointerPattern struct {
	kind      string
	downPoint wire.Point
	holdMs    float64
	fromPoint wire.Point
	toPoint   wire.Point
	pressMs   float64
}

// classifyPointerPattern reduces a validated single-pointer sequence
// to tap, longPress, or drag. Hybrid resolve semantics: a `down`
// target resolves once now; a `move` target re-resolves at its own
// step, right before the gesture fires.
func classifyPointerPattern(actions []script.PointerAction, ctx *CommandContext) (pointerPattern, error) {
	nonUp := actions[:len(actions)-1]

	if len(nonUp) == 1 {
		point, err := resolvePointerTarget(nonUp[0].Target, ctx)
		if err != nil {
			return pointerPattern{}, err
		}
		return pointerPattern{kind: "tap", downPoint: point}, nil
	}

	if len(nonUp) == 2 {
		down, second := nonUp[0], nonUp[1]
		downPoint, err := resolvePointerTarget(down.Target, ctx)
		if err != nil {
			return pointerPattern{}, err
		}
		if second.Type == "wait" {
			ms := 0.0
			if second.Ms != nil {
				ms = *second.Ms
			}
			return pointerPattern{kind: "longPress", downPoint: downPoint, holdMs: ms}, nil
		}
		if second.Type == "move" {
			toPoint, err := resolvePointerTarget(second.Target, ctx)
			if err != nil {
				return pointerPattern{}, err
			}
			return pointerPattern{kind: "drag", fromPoint: downPoint, toPoint: toPoint, pressMs: pointerDragMinPressMs}, nil
		}
		return pointerPattern{}, newPointerError("POINTER_PATTERN_NOT_EXPRESSIBLE",
			fmt.Sprintf("cannot express sequence: down + %s + up", second.Type))
	}

	if len(nonUp) == 3 {
		down, mid, last := nonUp[0], nonUp[1], nonUp[2]
		if mid.Type == "wait" && last.Type == "move" {
			downPoint, err := resolvePointerTarget(down.Target, ctx)
			if err != nil {
				return pointerPattern{}, err
			}
			toPoint, err := resolvePointerTarget(last.Target, ctx)
			if err != nil {
				return pointerPattern{}, err
			}
			ms := 0.0
			if mid.Ms != nil {
				ms = *mid.Ms
			}
			return pointerPattern{kind: "drag", fromPoint: downPoint, toPoint: toPoint, pressMs: ms}, nil
		}
		return pointerPattern{}, newPointerError("POINTER_PATTERN_NOT_EXPRESSIBLE", fmt.Sprintf(
			"cannot express sequence: down + %s + %s + up on the single-pointer path", mid.Type, last.Type))
	}

	return pointerPattern{}, newPointerError("POINTER_PATTERN_NOT_EXPRESSIBLE",
		"sequences with more than one `move` or interleaved waits "+
			"require the multi-waypoint synthesizer (not yet available)")
}

func dispatchPointerPattern(pattern pointerPattern, ctx *CommandContext) error {
	switch pattern.kind {
	case "tap":
		return ctx.Orchestra.TapAt(ctx.Context, pattern.downPoint)
	case "longPress":
		return ctx.Orchestra.LongPressAt(ctx.Context, pattern.downPoint, int(pattern.holdMs))
	case "drag":
		return ctx.Orchestra.SwipeAt(ctx.Context, pattern.fromPoint, pattern.toPoint, int(pattern.pressMs))
	}
	return nil
}
