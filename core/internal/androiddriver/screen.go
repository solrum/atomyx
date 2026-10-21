package androiddriver

import (
	"strconv"
	"strings"

	"dev.atomyx/internal/wire"
)

// parseBounds parses the wire format's "l,t,r,b" bounds attribute.
func parseBounds(raw string) (wire.Bounds, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return wire.Bounds{}, false
	}
	values := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return wire.Bounds{}, false
		}
		values[i] = v
	}
	return wire.Bounds{Left: values[0], Top: values[1], Right: values[2], Bottom: values[3]}, true
}

// rootDisplayBounds returns the tree root's own bounds, which current
// agents set to the full display rectangle. It returns false when the
// root has no parseable bounds with a positive area, as with older
// agents that leave the synthetic root without bounds.
func rootDisplayBounds(tree wire.TreeNodeWire) (wire.Bounds, bool) {
	raw, ok := tree.Attributes["bounds"]
	if !ok {
		return wire.Bounds{}, false
	}
	b, ok := parseBounds(raw)
	if !ok || (b.Right-b.Left) <= 0 || (b.Bottom-b.Top) <= 0 {
		return wire.Bounds{}, false
	}
	return b, true
}

// findScreenBounds walks the first layer of tree's children and
// returns the bounds with the LARGEST area: the main window dominates
// over any IME or overlay child. A bounds-bearing child with a zero or
// negative area never wins; it returns false when no child carries a
// parseable bounds attribute with a positive area.
func findScreenBounds(tree wire.TreeNodeWire) (wire.Bounds, bool) {
	var best wire.Bounds
	var bestArea float64
	found := false

	for _, child := range tree.Children {
		raw, ok := child.Attributes["bounds"]
		if !ok {
			continue
		}
		b, ok := parseBounds(raw)
		if !ok {
			continue
		}
		area := (b.Right - b.Left) * (b.Bottom - b.Top)
		if area > bestArea {
			best = b
			bestArea = area
			found = true
		}
	}

	return best, found
}
