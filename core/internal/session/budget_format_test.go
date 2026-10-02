package session

import "testing"

func TestFormatBudget(t *testing.T) {
	cases := map[int64]string{
		500:     "0.5 seconds",
		1000:    "1 second",
		1500:    "1.5 seconds",
		120_000: "120 seconds",
	}
	for ms, want := range cases {
		if got := formatBudget(ms); got != want {
			t.Errorf("formatBudget(%d) = %q, want %q", ms, got, want)
		}
	}
}
