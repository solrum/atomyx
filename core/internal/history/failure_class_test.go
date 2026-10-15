package history

import (
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
)

func failedRun(id string, outcome runfolder.Outcome, code string, start int64) runfolder.Meta {
	m := done(id, runfolder.KindScript, runfolder.ResultFailed, start, start+100)
	m.TestKey, m.Outcome, m.ErrorCode = "flows/a.yml", outcome, code
	return m
}

func TestQuerySplitsFailuresByClass(t *testing.T) {
	passed := done("r0", runfolder.KindScript, runfolder.ResultPassed, 500, 600)
	passed.TestKey = "flows/a.yml"
	source := fakeSource{metas: []runfolder.Meta{
		passed,
		failedRun("r1", runfolder.OutcomeFailed, "", 1000),
		failedRun("r2", runfolder.OutcomeFailed, diag.CodeAgentUnreachable, 2000),
		failedRun("r3", runfolder.OutcomeError, "", 3000),
		failedRun("r4", runfolder.OutcomeFailed, diag.CodeInvalidArgument, 4000),
	}}

	result, err := Query(source, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("tests = %+v", result.Tests)
	}
	s := result.Tests[0]
	if s.Runs != 5 || s.Failed != 4 || s.InfraFailures != 2 || s.TestFailures != 2 {
		t.Fatalf("stats = %+v", s)
	}
	if s.InfraFailureRate != 0.4 {
		t.Fatalf("infra failure rate = %v", s.InfraFailureRate)
	}
	if result.Totals != (Totals{Runs: 5, InfraFailures: 2, TestFailures: 2, InfraFailureRate: 0.4}) {
		t.Fatalf("totals = %+v", result.Totals)
	}
}

func TestQueryOnNoRunsHasZeroTotals(t *testing.T) {
	result, err := Query(fakeSource{}, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals != (Totals{}) {
		t.Fatalf("totals = %+v", result.Totals)
	}
}
