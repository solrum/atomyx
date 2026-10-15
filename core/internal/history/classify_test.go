package history

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
)

func TestEveryDiagCodeIsClassified(t *testing.T) {
	for _, code := range diag.Codes() {
		class, ok := codeClasses[code]
		if !ok {
			t.Errorf("diag code %s has no entry in codeClasses, decide whether it is infra or test", code)
			continue
		}
		if class != ClassInfra && class != ClassTest {
			t.Errorf("diag code %s has class %q", code, class)
		}
	}
	if len(codeClasses) != len(diag.Codes()) {
		t.Errorf("codeClasses lists %d codes, diag declares %d", len(codeClasses), len(diag.Codes()))
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		outcome runfolder.Outcome
		code    string
		want    Class
		failure bool
	}{
		{"error without code", runfolder.OutcomeError, "", ClassInfra, true},
		{"error with test code", runfolder.OutcomeError, diag.CodeInvalidArgument, ClassInfra, true},
		{"device lost", runfolder.OutcomeDeviceLost, "", ClassInfra, true},
		{"interrupted", runfolder.OutcomeInterrupted, "", ClassInfra, true},
		{"failed without code", runfolder.OutcomeFailed, "", ClassTest, true},
		{"failed with unknown code", runfolder.OutcomeFailed, "SOMETHING_NEW", ClassTest, true},
		{"failed with agent unreachable", runfolder.OutcomeFailed, diag.CodeAgentUnreachable, ClassInfra, true},
		{"failed with tool timeout", runfolder.OutcomeFailed, diag.CodeToolTimeout, ClassInfra, true},
		{"failed with device held", runfolder.OutcomeFailed, diag.CodeDeviceHeld, ClassInfra, true},
		{"failed with invalid argument", runfolder.OutcomeFailed, diag.CodeInvalidArgument, ClassTest, true},
		{"passed", runfolder.OutcomePassed, "", "", false},
		{"cancelled", runfolder.OutcomeCancelled, diag.CodeToolTimeout, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, failure := Classify(c.outcome, c.code)
			if got != c.want || failure != c.failure {
				t.Fatalf("Classify(%s, %q) = %q, %v, want %q, %v", c.outcome, c.code, got, failure, c.want, c.failure)
			}
		})
	}
}
