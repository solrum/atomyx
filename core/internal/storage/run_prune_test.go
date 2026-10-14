package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
)

const pruneDay = 24 * time.Hour

var pruneNow = time.Date(2026, time.June, 30, 12, 0, 0, 0, time.UTC)

// finishedMeta is a completed run that started age before pruneNow.
func finishedMeta(id string, age time.Duration) runfolder.Meta {
	start := pruneNow.Add(-age).UnixMilli()
	end := start + 1000
	return runfolder.Meta{
		SchemaVersion: runfolder.SchemaVersion, RunID: id, Kind: runfolder.KindScript,
		StartedAt: start, CompletedAt: &end, Result: runfolder.ResultPassed, Outcome: runfolder.OutcomePassed,
	}
}

func withParent(m runfolder.Meta, parent string) runfolder.Meta {
	m.ParentRunID = parent
	return m
}

func unitRoots(units []PruneUnit) []string {
	roots := []string{}
	for _, u := range units {
		roots = append(roots, u.Root.RunID)
	}
	return roots
}

func TestSelectPruneKeepsTheNewestTopLevelRuns(t *testing.T) {
	runs := []runfolder.Meta{
		finishedMeta("old", 30*pruneDay), finishedMeta("newest", pruneDay), finishedMeta("mid", 10*pruneDay),
	}

	got := SelectPrune(runs, nil, PruneCriteria{Keep: 2, Now: pruneNow})

	if want := []string{"old"}; !reflect.DeepEqual(unitRoots(got), want) {
		t.Fatalf("selected = %v, want %v", unitRoots(got), want)
	}
}

func TestSelectPruneOlderThanUsesTheInjectedClock(t *testing.T) {
	runs := []runfolder.Meta{finishedMeta("a", 5*pruneDay), finishedMeta("b", 20*pruneDay), finishedMeta("c", 40*pruneDay)}

	got := SelectPrune(runs, nil, PruneCriteria{OlderThan: 14 * pruneDay, Now: pruneNow})
	if want := []string{"b", "c"}; !reflect.DeepEqual(unitRoots(got), want) {
		t.Fatalf("selected = %v, want %v", unitRoots(got), want)
	}

	later := SelectPrune(runs, nil, PruneCriteria{OlderThan: 14 * pruneDay, Now: pruneNow.Add(10 * pruneDay)})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(unitRoots(later), want) {
		t.Fatalf("selected with a later clock = %v, want %v", unitRoots(later), want)
	}
}

func TestSelectPruneWithBothCriteriaDeletesOnlyWhatBothSelect(t *testing.T) {
	runs := []runfolder.Meta{finishedMeta("a", 2*pruneDay), finishedMeta("b", 3*pruneDay), finishedMeta("c", 30*pruneDay), finishedMeta("d", 40*pruneDay)}

	got := SelectPrune(runs, nil, PruneCriteria{Keep: 3, OlderThan: 14 * pruneDay, Now: pruneNow})

	if want := []string{"d"}; !reflect.DeepEqual(unitRoots(got), want) {
		t.Fatalf("selected = %v, want %v", unitRoots(got), want)
	}
}

func TestSelectPruneWithoutCriteriaSelectsNothing(t *testing.T) {
	runs := []runfolder.Meta{finishedMeta("a", 400*pruneDay)}

	if got := SelectPrune(runs, nil, PruneCriteria{Now: pruneNow}); len(got) != 0 {
		t.Fatalf("selected = %v, want none", unitRoots(got))
	}
}

func TestSelectPruneGroupsWorkersWithTheirParent(t *testing.T) {
	runs := []runfolder.Meta{
		finishedMeta("suite", 30*pruneDay),
		withParent(finishedMeta("w1", 30*pruneDay), "suite"),
		withParent(finishedMeta("w2", 30*pruneDay), "suite"),
		finishedMeta("fresh", pruneDay),
	}

	got := SelectPrune(runs, nil, PruneCriteria{Keep: 1, Now: pruneNow})

	if want := []string{"suite"}; !reflect.DeepEqual(unitRoots(got), want) {
		t.Fatalf("selected = %v, want %v", unitRoots(got), want)
	}
	if want := []string{"suite", "w1", "w2"}; !reflect.DeepEqual(got[0].Members, want) {
		t.Fatalf("members = %v, want %v", got[0].Members, want)
	}
}

func TestSelectPruneCountsAUnitOnceTowardKeep(t *testing.T) {
	runs := []runfolder.Meta{
		finishedMeta("suite", 5*pruneDay),
		withParent(finishedMeta("w1", 5*pruneDay), "suite"),
		withParent(finishedMeta("w2", 5*pruneDay), "suite"),
		finishedMeta("older", 9*pruneDay),
	}

	got := SelectPrune(runs, nil, PruneCriteria{Keep: 1, Now: pruneNow})

	if want := []string{"older"}; !reflect.DeepEqual(unitRoots(got), want) {
		t.Fatalf("selected = %v, want %v", unitRoots(got), want)
	}
}

func TestSelectPruneFollowsIndexWorkerLinks(t *testing.T) {
	runs := []runfolder.Meta{finishedMeta("suite", 30*pruneDay), finishedMeta("linked", 30*pruneDay)}
	links := map[string][]string{"suite": {"linked", "not-listed"}}

	got := SelectPrune(runs, links, PruneCriteria{OlderThan: pruneDay, Now: pruneNow})

	if len(got) != 1 || !reflect.DeepEqual(got[0].Members, []string{"suite", "linked"}) {
		t.Fatalf("units = %+v, want one unit of suite and linked", got)
	}
}

func TestSelectPruneTreatsAWorkerWithoutItsParentAsItsOwnUnit(t *testing.T) {
	runs := []runfolder.Meta{withParent(finishedMeta("orphan", 30*pruneDay), "gone"), finishedMeta("fresh", pruneDay)}

	got := SelectPrune(runs, nil, PruneCriteria{OlderThan: 14 * pruneDay, Now: pruneNow})

	if len(got) != 1 || got[0].Root.RunID != "orphan" || !reflect.DeepEqual(got[0].Members, []string{"orphan"}) {
		t.Fatalf("units = %+v, want the orphan alone", got)
	}
}

func TestSelectPruneFlagsAUnitWithALiveMember(t *testing.T) {
	live := withParent(finishedMeta("w-live", 30*pruneDay), "suite")
	live.CompletedAt, live.Result, live.Outcome = nil, "", ""
	runs := []runfolder.Meta{finishedMeta("suite", 30*pruneDay), live, finishedMeta("quiet", 31*pruneDay)}

	got := SelectPrune(runs, nil, PruneCriteria{OlderThan: pruneDay, Now: pruneNow})

	if len(got) != 2 {
		t.Fatalf("units = %v", unitRoots(got))
	}
	byRoot := map[string]bool{}
	for _, u := range got {
		byRoot[u.Root.RunID] = u.Live
	}
	if !byRoot["suite"] || byRoot["quiet"] {
		t.Fatalf("live flags = %v, want suite live and quiet not", byRoot)
	}
}

func TestSelectPruneToleratesParentCycles(t *testing.T) {
	runs := []runfolder.Meta{withParent(finishedMeta("a", 30*pruneDay), "b"), withParent(finishedMeta("b", 30*pruneDay), "a")}

	if got := SelectPrune(runs, nil, PruneCriteria{OlderThan: pruneDay, Now: pruneNow}); len(got) != 0 {
		t.Fatalf("units = %v, want none for a cycle with no root", unitRoots(got))
	}
}

func writePruneRun(t *testing.T, root string, m runfolder.Meta) {
	t.Helper()
	dir := runfolder.RunDir(root, m.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runfolder.MetaFileName), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePruneIndex(t *testing.T, root, runID string, workerIDs ...string) {
	t.Helper()
	attempts := []runfolder.IndexAttempt{}
	for i, w := range workerIDs {
		attempts = append(attempts, runfolder.IndexAttempt{Number: i + 1, Result: runfolder.ResultPassed, WorkerRunID: w})
	}
	index := runfolder.Index{
		SchemaVersion: runfolder.IndexSchemaVersion, RunID: runID,
		Entries: []runfolder.IndexEntry{{Path: "a.yml", Status: runfolder.ResultPassed, Attempts: attempts}},
	}
	body, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runfolder.RunDir(root, runID), runfolder.IndexFileName), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runExists(root, id string) bool {
	_, err := os.Stat(runfolder.RunDir(root, id))
	return err == nil
}

func TestPlanPruneReadsWorkerLinksFromTheIndex(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, withKind(finishedMeta("suite", 30*pruneDay), runfolder.KindSuite))
	writePruneIndex(t, root, "suite", "linked-worker")
	writePruneRun(t, root, finishedMeta("linked-worker", 30*pruneDay))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))

	plan, err := c.PlanPrune(PruneCriteria{Keep: 1, Now: pruneNow})

	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Units) != 1 || !reflect.DeepEqual(plan.Units[0].Members, []string{"suite", "linked-worker"}) {
		t.Fatalf("units = %+v, want suite and its linked worker as one unit", plan.Units)
	}
}

func withKind(m runfolder.Meta, kind runfolder.Kind) runfolder.Meta {
	m.Kind = kind
	return m
}

func TestDeletePruneUnitRemovesTheParentAndEveryWorker(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, withKind(finishedMeta("suite", 30*pruneDay), runfolder.KindSuite))
	writePruneIndex(t, root, "suite", "linked-worker")
	writePruneRun(t, root, finishedMeta("linked-worker", 30*pruneDay))
	writePruneRun(t, root, withParent(finishedMeta("parent-worker", 30*pruneDay), "suite"))
	writePruneRun(t, root, finishedMeta("fresh", pruneDay))
	plan, err := c.PlanPrune(PruneCriteria{Keep: 1, Now: pruneNow})
	if err != nil || len(plan.Units) != 1 {
		t.Fatalf("PlanPrune = %+v, %v", plan, err)
	}

	if err := c.DeletePruneUnit(plan.Units[0]); err != nil {
		t.Fatalf("DeletePruneUnit: %v", err)
	}

	for _, id := range []string{"suite", "linked-worker", "parent-worker"} {
		if runExists(root, id) {
			t.Errorf("run %s still exists", id)
		}
	}
	if !runExists(root, "fresh") {
		t.Error("the kept run was removed")
	}
}

func TestPlanPruneKeepsAWholeUnitWhenTheParentIsKept(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, withKind(finishedMeta("suite", pruneDay), runfolder.KindSuite))
	writePruneIndex(t, root, "suite", "linked-worker")
	writePruneRun(t, root, finishedMeta("linked-worker", 90*pruneDay))
	writePruneRun(t, root, withParent(finishedMeta("parent-worker", 90*pruneDay), "suite"))

	plan, err := c.PlanPrune(PruneCriteria{OlderThan: 14 * pruneDay, Now: pruneNow})

	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Units) != 0 {
		t.Fatalf("units = %+v, want none because the parent is recent", unitRoots(plan.Units))
	}
}

func TestPlanPruneNeverSelectsSkippedEntries(t *testing.T) {
	root := t.TempDir()
	c := liveCatalog(t, root)
	writePruneRun(t, root, finishedMeta("old", 90*pruneDay))
	damaged := runfolder.RunDir(root, "damaged")
	if err := os.MkdirAll(damaged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(damaged, runfolder.MetaFileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := c.PlanPrune(PruneCriteria{OlderThan: pruneDay, Now: pruneNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range plan.Units {
		if err := c.DeletePruneUnit(u); err != nil {
			t.Fatal(err)
		}
	}

	if len(plan.Skipped) != 1 || plan.Skipped[0].ID != "damaged" {
		t.Fatalf("skipped = %+v, want the damaged run", plan.Skipped)
	}
	if !runExists(root, "damaged") {
		t.Error("the skipped run was deleted")
	}
	if runExists(root, "old") {
		t.Error("the old run was not deleted")
	}
}

func TestDeletePruneUnitRefusesAUnitWithALiveWorkerAndRemovesNothing(t *testing.T) {
	root := t.TempDir()
	c := NewRunCatalog(mustStorage(t, root), root, liveSet{"live-worker": true})
	live := withParent(finishedMeta("live-worker", 30*pruneDay), "suite")
	live.CompletedAt, live.Result, live.Outcome = nil, "", ""
	writePruneRun(t, root, finishedMeta("suite", 30*pruneDay))
	writePruneRun(t, root, live)
	unit := PruneUnit{Root: finishedMeta("suite", 30*pruneDay), Members: []string{"suite", "live-worker"}}

	err := c.DeletePruneUnit(unit)

	if !errors.Is(err, ErrRunLive) {
		t.Fatalf("DeletePruneUnit = %v, want ErrRunLive", err)
	}
	if !runExists(root, "suite") || !runExists(root, "live-worker") {
		t.Error("a member of a unit with a live run was removed")
	}
}

func mustStorage(t *testing.T, root string) Storage {
	t.Helper()
	fs, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
