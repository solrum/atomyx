package expression

import (
	"math"
	"testing"
)

// pointOne and pointTwo are runtime float64 values so their sum is
// computed with actual IEEE 754 rounding. A literal 0.1 + 0.2 in Go
// source is folded at compile time with arbitrary-precision constant
// arithmetic and would round to the nearest float64 of exact 0.3
// instead of reproducing the classic floating-point rounding artifact.
var (
	pointOne             = 0.1
	pointTwo             = 0.2
	pointOnePlusPointTwo = pointOne + pointTwo
)

// Expected strings are Node-verified ground truth (node -e
// "console.log((X).toString())"), not transcribed by hand, so a
// mismatch here means FormatNumber is wrong, not the test.
func TestFormatNumber(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "0"},
		{"negative zero", math.Copysign(0, -1), "0"},
		{"one", 1, "1"},
		{"negative one", -1, "-1"},
		{"five", 5, "5"},
		{"five point five", 5.5, "5.5"},
		{"point one", 0.1, "0.1"},
		{"one two three point four five six", 123.456, "123.456"},
		{"negative point five", -0.5, "-0.5"},
		{"one third", 1.0 / 3.0, "0.3333333333333333"},
		{"point one plus point two", pointOnePlusPointTwo, "0.30000000000000004"},
		{"hundred thousand", 100000, "100000"},
		{"one million", 1000000, "1000000"},
		{"1e20", 1e20, "100000000000000000000"},
		{"just under 1e21", 9.999999999999999e20, "999999999999999900000"},
		{"1e21", 1e21, "1e+21"},
		{"just over 1e21", 1.0000000000000001e21, "1.0000000000000001e+21"},
		{"negative 1e21", -1e21, "-1e+21"},
		{"1e-6", 1e-6, "0.000001"},
		{"1e-7", 1e-7, "1e-7"},
		{"negative 1e-7", -1e-7, "-1e-7"},
		{"smallest denormal", 5e-324, "5e-324"},
		{"largest finite", 1.7976931348623157e308, "1.7976931348623157e+308"},
		{"1e100", 1e100, "1e+100"},
		{"1.5e300", 1.5e300, "1.5e+300"},
		{"2^53", math.Pow(2, 53), "9007199254740992"},
		{"positive infinity", math.Inf(1), "Infinity"},
		{"negative infinity", math.Inf(-1), "-Infinity"},
		{"not a number", math.NaN(), "NaN"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatNumber(c.in); got != c.want {
				t.Errorf("FormatNumber(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
