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
	if result.Totals != (Totals{Runs: 5, Attempts: 5, InfraFailures: 2, TestFailures: 2, InfraFailureRate: 0.4}) {
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

func abortedMeta(id string, outcome runfolder.Outcome, start int64) runfolder.Meta {
	m := runfolder.Meta{RunID: id, Kind: runfolder.KindScript, TestKey: "flows/a.yml", StartedAt: start, Result: runfolder.ResultCancelled, Outcome: outcome}
	if outcome == runfolder.OutcomeDeviceLost {
		end := start + 50
		m.CompletedAt = &end
	}
	return m
}

func TestAbortedRunsCountAsInfraFailuresOutsideRuns(t *testing.T) {
	passed := done("r1", runfolder.KindScript, runfolder.ResultPassed, 1000, 1100)
	passed.TestKey = "flows/a.yml"
	source := fakeSource{metas: []runfolder.Meta{
		passed,
		abortedMeta("r2", runfolder.OutcomeDeviceLost, 2000),
		abortedMeta("r3", runfolder.OutcomeInterrupted, 3000),
		abortedMeta("r4", runfolder.OutcomeCancelled, 4000),
		failedRun("r5", runfolder.OutcomeFailed, "", 5000),
	}}
	result, err := Query(source, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	s := result.Tests[0]
	if s.Runs != 2 || s.Passed != 1 || s.Failed != 1 || s.AbortedRuns != 2 || s.Attempts != 4 {
		t.Fatalf("stats = %+v", s)
	}
	if s.InfraFailures != 2 || s.TestFailures != 1 || s.InfraFailureRate != 0.5 {
		t.Fatalf("failure split = %+v", s)
	}
	if len(s.LastOutcomes) != 2 {
		t.Fatalf("aborted runs leaked into lastOutcomes: %v", s.LastOutcomes)
	}
	if result.Totals != (Totals{Runs: 2, Attempts: 4, AbortedRuns: 2, InfraFailures: 2, TestFailures: 1, InfraFailureRate: 0.5}) {
		t.Fatalf("totals = %+v", result.Totals)
	}
}

func TestCancelledRunIsInNeitherCount(t *testing.T) {
	source := fakeSource{metas: []runfolder.Meta{abortedMeta("r1", runfolder.OutcomeCancelled, 1000)}}
	result, err := Query(source, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tests) != 0 || result.Totals != (Totals{}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestOnlyAbortedRunsReportZeroRunsWithoutNaN(t *testing.T) {
	source := fakeSource{metas: []runfolder.Meta{abortedMeta("r1", runfolder.OutcomeDeviceLost, 1000)}}
	result, err := Query(source, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	s := result.Tests[0]
	if s.Runs != 0 || s.Attempts != 1 || s.InfraFailureRate != 1 || s.FlakyRate != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestLastNKeepsFinishedRunsUnchangedAndAbortedWithinTheirSpan(t *testing.T) {
	var metas []runfolder.Meta
	for i, start := range []int64{1000, 2000, 3000, 4000} {
		m := done("p"+string(rune('a'+i)), runfolder.KindScript, runfolder.ResultPassed, start, start+10)
		m.TestKey = "flows/a.yml"
		metas = append(metas, m)
	}
	metas = append(metas,
		abortedMeta("old", runfolder.OutcomeDeviceLost, 1500),
		abortedMeta("new", runfolder.OutcomeDeviceLost, 3500))
	result, err := Query(fakeSource{metas: metas}, Filter{Window: Window{LastN: 2}})
	if err != nil {
		t.Fatal(err)
	}
	s := result.Tests[0]
	if s.Runs != 2 || s.AbortedRuns != 1 || s.Attempts != 3 {
		t.Fatalf("stats = %+v", s)
	}
}
