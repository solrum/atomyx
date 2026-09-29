package expression

import (
	"math"
	"strconv"
	"strings"
)

// FormatNumber renders f in the shortest decimal form that round-trips
// back to the same 64-bit floating-point value: no trailing zeros, no
// leading zeros beyond one digit, decimal notation for magnitudes
// roughly between a millionth and 10^21, and exponential notation
// (with an unpadded, explicitly signed exponent) outside that range.
// string(x) on a KindNumber value depends on this exact rendering,
// since script authors compare the resulting string against literal
// expectations.
func FormatNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case f == 0:
		return "0"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}

	sci := strconv.FormatFloat(f, 'e', -1, 64)
	eIdx := strings.LastIndexByte(sci, 'e')
	mantissa := sci[:eIdx]
	decExp, err := strconv.Atoi(sci[eIdx+1:])
	if err != nil {
		// strconv.FormatFloat with the 'e' verb always produces a
		// parseable exponent; a failure here would mean the standard
		// library's own contract broke.
		panic(err)
	}

	if decExp >= -6 && decExp <= 20 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}

	sign := "+"
	if decExp < 0 {
		sign = "-"
		decExp = -decExp
	}
	return mantissa + "e" + sign + strconv.Itoa(decExp)
}
