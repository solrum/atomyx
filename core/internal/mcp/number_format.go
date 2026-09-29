package mcp

import (
	"math"
	"strconv"
)

// formatNumber renders f as a human-readable number: no trailing ".0"
// for an integral value, and the shortest round-trip decimal
// representation otherwise. Tool result strings that echo a
// caller-supplied coordinate use this so a whole-number screen
// coordinate, the common case, reads as a plain integer instead of
// carrying a spurious ".0".
func formatNumber(f float64) string {
	if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) &&
		math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
