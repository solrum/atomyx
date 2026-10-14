package storage

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewPruneCriteriaConvertsDaysToADuration(t *testing.T) {
	got := NewPruneCriteria(3, 2, pruneNow)
	if got.Keep != 3 || got.OlderThan != 48*time.Hour || !got.Now.Equal(pruneNow) {
		t.Fatalf("criteria = %+v", got)
	}
	if zero := NewPruneCriteria(0, 0, pruneNow); zero.Keep != 0 || zero.OlderThan != 0 {
		t.Fatalf("unset bounds = %+v, want zero", zero)
	}
}

func TestApplyPruneDeletesSelectedUnitsAndCountsRuns(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, finishedMeta("old", 30*pruneDay))
	writePruneRun(t, root, withParent(finishedMeta("old-worker", 30*pruneDay), "old"))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))

	result, err := c.ApplyPrune(PruneCriteria{Keep: 1, Now: pruneNow}, false)

	if err != nil {
		t.Fatal(err)
	}
	if result.RunsDeleted != 2 || len(result.Deleted()) != 1 || result.Failed() || len(result.Warnings()) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if runExists(root, "old") || runExists(root, "old-worker") || !runExists(root, "fresh") {
		t.Fatal("wrong runs deleted")
	}
}

func TestApplyPruneDryRunDeletesNothing(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, finishedMeta("old", 30*pruneDay))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))

	result, err := c.ApplyPrune(PruneCriteria{Keep: 1, Now: pruneNow}, true)

	if err != nil || len(result.Outcomes) != 1 || result.Outcomes[0].Action != PruneWouldDelete || result.RunsDeleted != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if !runExists(root, "old") {
		t.Fatal("dry run deleted a run")
	}
}

func TestApplyPruneKeepsALiveRunAndWarns(t *testing.T) {
	root := t.TempDir()
	c := NewRunCatalog(mustStorage(t, root), root, &flipLive{id: "old"})
	writePruneRun(t, root, finishedMeta("old", 30*pruneDay))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))

	result, err := c.ApplyPrune(PruneCriteria{Keep: 1, Now: pruneNow}, false)

	if err != nil {
		t.Fatal(err)
	}
	warnings := result.Warnings()
	if !result.Failed() || len(warnings) != 1 || !strings.Contains(warnings[0], "old") || !strings.Contains(warnings[0], "still being written") {
		t.Fatalf("warnings = %v", warnings)
	}
	if !runExists(root, "old") {
		t.Fatal("a live run was deleted")
	}
}

func TestPruneResultWarningsNameAMigrationAndUnreadableRecords(t *testing.T) {
	unit := PruneUnit{Root: finishedMeta("busy", pruneDay)}
	result := PruneResult{
		Skipped: 2,
		Outcomes: []PruneOutcome{
			{Unit: unit, Action: PruneFailed, Err: fmt.Errorf("run busy: %w", ErrMigrationInProgress)},
			{Unit: PruneUnit{Root: finishedMeta("bad", pruneDay)}, Action: PruneFailed, Err: errors.New("disk")},
		},
	}

	warnings := result.Warnings()

	want := []string{"migration is in progress", "could not be deleted: disk", "2 run records"}
	if len(warnings) != len(want) {
		t.Fatalf("warnings = %v", warnings)
	}
	for i, w := range want {
		if !strings.Contains(warnings[i], w) {
			t.Errorf("warning %d = %q, want it to contain %q", i, warnings[i], w)
		}
	}
}

func TestPruneRootIgnoresAMissingRootAndPrunesAnExistingOne(t *testing.T) {
	missing := t.TempDir() + "/absent"
	if result, err := PruneRoot(missing, PruneCriteria{Keep: 1, Now: pruneNow}); err != nil || len(result.Outcomes) != 0 {
		t.Fatalf("missing root = %+v, %v", result, err)
	}

	root := t.TempDir()
	writePruneRun(t, root, finishedMeta("old", 30*pruneDay))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))
	result, err := PruneRoot(root, PruneCriteria{Keep: 1, Now: pruneNow})
	if err != nil || result.RunsDeleted != 1 || runExists(root, "old") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

const planCostRuns = 1000

func TestPlanPruneOverAThousandRunsSelectsEveryRunOutsideKeep(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	for i := 0; i < planCostRuns; i++ {
		writePruneRun(t, root, finishedMeta(fmt.Sprintf("run-%04d", i), time.Duration(i+1)*time.Hour))
	}

	start := time.Now()
	plan, err := c.PlanPrune(PruneCriteria{Keep: 50, Now: pruneNow})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Units) != planCostRuns-50 {
		t.Fatalf("units = %d, want %d", len(plan.Units), planCostRuns-50)
	}
	t.Logf("PlanPrune over %d runs took %s", planCostRuns, elapsed)
}
