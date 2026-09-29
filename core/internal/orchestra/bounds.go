package orchestra

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"dev.atomyx/internal/wire"
)

// ParseBounds decodes the "left,top,right,bottom" string stored
// under the "bounds" attribute into a wire.Bounds. It returns
// (wire.Bounds{}, false) when s does not split into exactly four
// comma-separated numbers, or when any of them is not a finite
// number. Bounds strings originate from the device driver's
// accessibility-tree attributes, so this parser stays as permissive
// about numeric formatting as parseJSNumber below rather than
// assuming the driver always emits the same canonical formatting.
func ParseBounds(s string) (wire.Bounds, bool) {
	if s == "" {
		return wire.Bounds{}, false
	}
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return wire.Bounds{}, false
	}
	left, leftOK := parseJSNumber(parts[0])
	top, topOK := parseJSNumber(parts[1])
	if !leftOK || !topOK {
		return wire.Bounds{}, false
	}
	right, rightOK := parseJSNumber(parts[2])
	bottom, bottomOK := parseJSNumber(parts[3])
	if !rightOK || !bottomOK {
		return wire.Bounds{}, false
	}
	return wire.Bounds{Left: left, Top: top, Right: right, Bottom: bottom}, true
}

// BoundsCenter returns the geometric center point of b.
func BoundsCenter(b wire.Bounds) wire.Point {
	return wire.Point{X: (b.Left + b.Right) / 2, Y: (b.Top + b.Bottom) / 2}
}

// BoundsAnchor resolves one of the nine standard rect anchors, including the
// four corners, the four edge midpoints, or the center, to a point
// on bounds. It returns an error for any other anchor name, so a
// caller building one outside the schema's validated set (a pointer
// step assembled programmatically) gets an explicit failure instead
// of a wrong point.
func BoundsAnchor(bounds wire.Bounds, anchor string) (wire.Point, error) {
	switch anchor {
	case "center":
		return BoundsCenter(bounds), nil
	case "topLeft":
		return wire.Point{X: bounds.Left, Y: bounds.Top}, nil
	case "topRight":
		return wire.Point{X: bounds.Right, Y: bounds.Top}, nil
	case "bottomLeft":
		return wire.Point{X: bounds.Left, Y: bounds.Bottom}, nil
	case "bottomRight":
		return wire.Point{X: bounds.Right, Y: bounds.Bottom}, nil
	case "top":
		return wire.Point{X: (bounds.Left + bounds.Right) / 2, Y: bounds.Top}, nil
	case "bottom":
		return wire.Point{X: (bounds.Left + bounds.Right) / 2, Y: bounds.Bottom}, nil
	case "left":
		return wire.Point{X: bounds.Left, Y: (bounds.Top + bounds.Bottom) / 2}, nil
	case "right":
		return wire.Point{X: bounds.Right, Y: (bounds.Top + bounds.Bottom) / 2}, nil
	default:
		return wire.Point{}, fmt.Errorf(
			"unknown anchor %q. Valid anchors: center, topLeft, topRight, bottomLeft, bottomRight, top, bottom, left, right",
			anchor)
	}
}

// BoundsContain reports whether the point (x, y) falls inside b,
// treating the left and top edges as inclusive and the right and
// bottom edges as exclusive.
func BoundsContain(b wire.Bounds, x, y float64) bool {
	return x >= b.Left && x < b.Right && y >= b.Top && y < b.Bottom
}

// BoundsIntersect reports whether a and b overlap by any nonzero
// area. Two rects that only share an edge or a corner do not
// intersect under this definition.
func BoundsIntersect(a, b wire.Bounds) bool {
	return !(a.Right <= b.Left || b.Right <= a.Left || a.Bottom <= b.Top || b.Bottom <= a.Top)
}

// parseJSNumber parses a trimmed numeric string the way JavaScript's
// Number() coerces it: an empty string is 0 (not an error), and any
// other value that fails to parse, or parses to NaN or an infinite
// value, reports ok=false, matching Number.isFinite's rejection of
// both.
func parseJSNumber(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}
