package setup

import "testing"

func TestReportOKIsTrueOnlyWhenEveryStepIsOkOrFixed(t *testing.T) {
	cases := []struct {
		statuses []Status
		want     bool
	}{
		{nil, true},
		{[]Status{StatusOK, StatusFixed}, true},
		{[]Status{StatusOK, StatusSkipped}, false},
		{[]Status{StatusOK, StatusManual}, false},
		{[]Status{StatusFailed}, false},
	}
	for _, c := range cases {
		var r Report
		for _, s := range c.statuses {
			r.Steps = append(r.Steps, StepReport{Status: s})
		}
		if r.OK() != c.want {
			t.Errorf("OK() for %v = %v, want %v", c.statuses, r.OK(), c.want)
		}
		wantCode := 1
		if c.want {
			wantCode = 0
		}
		if r.ExitCode() != wantCode {
			t.Errorf("ExitCode() for %v = %d, want %d", c.statuses, r.ExitCode(), wantCode)
		}
	}
}
