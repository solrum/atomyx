package androiddriver

import (
	"strconv"
	"strings"

	"dev.atomyx/internal/wire"
)

// findScreenBounds walks the first layer of tree's children looking
// for a "bounds" attribute, parsed from the wire format's "l,t,r,b"
// string, and returns the LARGEST bounds by area: the main window
// dominates over any IME or overlay child. bestArea starts at zero so
// a bounds-bearing child with a zero or negative area never wins;
// it returns false when no child carries a parseable bounds attribute
// with a positive area.
func findScreenBounds(tree wire.TreeNodeWire) (wire.Bounds, bool) {
	var best wire.Bounds
	var bestArea float64
	found := false

	for _, child := range tree.Children {
		raw, ok := child.Attributes["bounds"]
		if !ok {
			continue
		}
		parts := strings.Split(raw, ",")
		if len(parts) != 4 {
			continue
		}
		values := make([]float64, 4)
		valid := true
		for i, p := range parts {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				valid = false
				break
			}
			values[i] = v
		}
		if !valid {
			continue
		}
		b := wire.Bounds{Left: values[0], Top: values[1], Right: values[2], Bottom: values[3]}
		area := (b.Right - b.Left) * (b.Bottom - b.Top)
		if area > bestArea {
			best = b
			bestArea = area
			found = true
		}
	}

	return best, found
}
