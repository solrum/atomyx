package setup

import "testing"

func TestStatusSatisfied(t *testing.T) {
	cases := map[Status]bool{
		StatusOK:      true,
		StatusFixed:   true,
		StatusManual:  false,
		StatusFailed:  false,
		StatusSkipped: false,
	}
	for status, want := range cases {
		if got := status.Satisfied(); got != want {
			t.Errorf("%s.Satisfied() = %v, want %v", status, got, want)
		}
	}
}
