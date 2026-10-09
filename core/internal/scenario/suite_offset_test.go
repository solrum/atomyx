package scenario

import (
	"context"
	"testing"
)

func TestSuiteRunner_EntryOffsetShiftsTheEntryPositionAHostSees(t *testing.T) {
	orch, d := newEmptyScreenFixture(t)
	d.StageHierarchy(testHierarchy)
	rec := &attemptRecorder{}
	r := newRetrySuite(orch, rec.start, nil)
	entries := []SuiteEntry{{Path: "a.yml", Content: passYaml}, {Path: "b.yml", Content: passYaml}}

	if _, err := r.Run(context.Background(), entries, &SuiteRunOptions{EntryOffset: 4}); err != nil {
		t.Fatal(err)
	}
	if rec.attempts() != "4.1,5.1" {
		t.Errorf("hook attempts = %s, want 4.1,5.1", rec.attempts())
	}
	rec.targets = nil
	if _, err := r.Run(context.Background(), entries[:1], &SuiteRunOptions{EntryOffset: -3}); err != nil {
		t.Fatal(err)
	}
	if rec.attempts() != "0.1" {
		t.Errorf("a negative offset gave %s, want 0.1", rec.attempts())
	}
}

func TestSummarizeCountsPassedFlakyQuarantinedAndFailedEntries(t *testing.T) {
	name := "nightly"
	result := Summarize([]SuiteEntryResult{
		{Path: "p", Status: EntryPassed},
		{Path: "f", Status: EntryPassed, Flaky: true},
		{Path: "q", Status: EntryFailed, Quarantined: true},
		{Path: "x", Status: EntryFailed},
	}, &name, 90)

	if result.Total != 4 || result.Passed != 2 || result.Failed != 1 || result.Flaky != 1 || result.QuarantinedFailures != 1 {
		t.Errorf("counts = %+v", result)
	}
	if result.Ok || result.DurationMs != 90 || result.SuiteName == nil || *result.SuiteName != "nightly" {
		t.Errorf("result = %+v, want a failed suite named nightly taking 90 ms", result)
	}
	if !Summarize([]SuiteEntryResult{{Status: EntryFailed, Quarantined: true}}, nil, 0).Ok {
		t.Error("a suite whose only failure is quarantined is not ok")
	}
}
