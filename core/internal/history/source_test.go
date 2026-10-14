package history

import (
	"errors"
	"io/fs"
	"reflect"
	"sort"
	"testing"

	"dev.atomyx/internal/runfolder"
)

type fakeSource struct {
	metas   []runfolder.Meta
	indexes map[string]runfolder.Index
	skipped int
	err     error
}

func (f fakeSource) Metas() ([]runfolder.Meta, int, error) { return f.metas, f.skipped, f.err }

func (f fakeSource) ReadIndex(runID string) (runfolder.Index, error) {
	index, ok := f.indexes[runID]
	if !ok {
		return runfolder.Index{}, fs.ErrNotExist
	}
	return index, nil
}

func done(id string, kind runfolder.Kind, result runfolder.Result, start, end int64) runfolder.Meta {
	return runfolder.Meta{RunID: id, Kind: kind, StartedAt: start, CompletedAt: &end, Result: result}
}

func keysOf(records []Record) []string {
	var keys []string
	for _, r := range records {
		keys = append(keys, r.Key+"="+string(r.Outcome))
	}
	sort.Strings(keys)
	return keys
}

func TestCollectScriptRunsUseTestKeyThenScriptPath(t *testing.T) {
	keyed := done("r1", runfolder.KindScript, runfolder.ResultPassed, 1000, 1500)
	keyed.TestKey, keyed.ScriptPath = "flows/a.yml", "/abs/flows/a.yml"
	legacy := done("r2", runfolder.KindScript, runfolder.ResultFailed, 2000, 2300)
	legacy.ScriptPath = "/abs/flows/b.yml"
	anonymous := done("r3", runfolder.KindScript, runfolder.ResultPassed, 3000, 3100)

	records, err := Collect(fakeSource{metas: []runfolder.Meta{keyed, legacy, anonymous}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/abs/flows/b.yml=failed", "flows/a.yml=passed"}
	if got := keysOf(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
	for _, r := range records {
		if !r.HasDuration {
			t.Fatalf("script record %+v has no duration", r)
		}
	}
}

func TestCollectSkipsUnfinishedAndCancelledRuns(t *testing.T) {
	running := runfolder.Meta{RunID: "r1", Kind: runfolder.KindScript, TestKey: "a", StartedAt: 1}
	cancelled := done("r2", runfolder.KindScript, runfolder.ResultCancelled, 1, 2)
	cancelled.TestKey = "a"
	records, err := Collect(fakeSource{metas: []runfolder.Meta{running, cancelled}})
	if err != nil || len(records) != 0 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestCollectSuiteContributesIndexEntriesNotItsOwnMeta(t *testing.T) {
	suite := done("s1", runfolder.KindSuite, runfolder.ResultPassed, 5000, 9000)
	suite.TestKey = "should-not-count"
	source := fakeSource{
		metas: []runfolder.Meta{suite},
		indexes: map[string]runfolder.Index{"s1": {SchemaVersion: 1, RunID: "s1", Entries: []runfolder.IndexEntry{
			{Path: "a.yml", TestKey: "flows/a.yml", Status: runfolder.ResultPassed, Attempts: []runfolder.IndexAttempt{{Number: 1}}},
			{Path: "b.yml", Status: runfolder.ResultPassed, Flaky: true, Attempts: []runfolder.IndexAttempt{{Number: 1}}},
			{Path: "c.yml", TestKey: "flows/c.yml", Status: runfolder.ResultFailed, Attempts: []runfolder.IndexAttempt{{Number: 1}}},
		}}},
	}
	records, err := Collect(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b.yml=flaky", "flows/a.yml=passed", "flows/c.yml=failed"}
	if got := keysOf(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
	for _, r := range records {
		if r.HasDuration || r.StartedAtMs != 5000 || r.RunID != "s1" {
			t.Fatalf("suite entry record = %+v", r)
		}
	}
}

func TestCollectSuiteWithoutIndexIsKeyedByItsOwnMeta(t *testing.T) {
	scenario := done("s1", runfolder.KindSuite, runfolder.ResultFailed, 1, 11)
	scenario.TestKey = "scenarios/checkout.yml"
	records, err := Collect(fakeSource{metas: []runfolder.Meta{scenario}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(records), []string{"scenarios/checkout.yml=failed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
}

func TestCollectCountsAShardedEntryOnceThroughItsParent(t *testing.T) {
	parent := done("p1", runfolder.KindSuite, runfolder.ResultPassed, 1000, 2000)
	worker := done("w1", runfolder.KindSuite, runfolder.ResultPassed, 1100, 1900)
	worker.ParentRunID = "p1"
	entry := runfolder.IndexEntry{Path: "a.yml", TestKey: "a.yml", Status: runfolder.ResultPassed, Attempts: []runfolder.IndexAttempt{{Number: 1}}}
	source := fakeSource{
		metas: []runfolder.Meta{parent, worker},
		indexes: map[string]runfolder.Index{
			"p1": {SchemaVersion: 1, Entries: []runfolder.IndexEntry{entry}},
			"w1": {SchemaVersion: 1, Entries: []runfolder.IndexEntry{entry}},
		},
	}
	records, err := Collect(source)
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}

	source.metas = []runfolder.Meta{worker}
	records, err = Collect(source)
	if err != nil || len(records) != 1 {
		t.Fatalf("orphan worker should still count: %+v, err = %v", records, err)
	}
}

func TestCollectReportsSourceFailures(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Collect(fakeSource{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("list error = %v", err)
	}
	suite := done("s1", runfolder.KindSuite, runfolder.ResultPassed, 1, 2)
	failing := failingIndex{fakeSource{metas: []runfolder.Meta{suite}}, boom}
	if _, err := Collect(failing); !errors.Is(err, boom) {
		t.Fatalf("index error = %v", err)
	}
}

type failingIndex struct {
	fakeSource
	err error
}

func (f failingIndex) ReadIndex(string) (runfolder.Index, error) { return runfolder.Index{}, f.err }

func TestCollectCancelledSuiteWithIndexContributesNothing(t *testing.T) {
	suite := done("s1", runfolder.KindSuite, runfolder.ResultCancelled, 1, 2)
	source := fakeSource{
		metas: []runfolder.Meta{suite},
		indexes: map[string]runfolder.Index{"s1": {Entries: []runfolder.IndexEntry{
			{Path: "a.yml", Status: runfolder.ResultPassed, Attempts: []runfolder.IndexAttempt{{Number: 1}}},
		}}},
	}
	records, err := Collect(source)
	if err != nil || len(records) != 0 {
		t.Fatalf("records = %+v, err = %v", records, err)
	}
}

func TestCollectSkipsIndexEntriesWithoutAttempts(t *testing.T) {
	suite := done("s1", runfolder.KindSuite, runfolder.ResultFailed, 1, 2)
	source := fakeSource{
		metas: []runfolder.Meta{suite},
		indexes: map[string]runfolder.Index{"s1": {Entries: []runfolder.IndexEntry{
			{Path: "ran.yml", Status: runfolder.ResultPassed, Attempts: []runfolder.IndexAttempt{{Number: 1}}},
			{Path: "never.yml", Status: runfolder.ResultFailed},
		}}},
	}
	records, err := Collect(source)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(records), []string{"ran.yml=passed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
}

func TestCollectWorkerWithParentPresentButNoIndexIsSkipped(t *testing.T) {
	parent := done("p1", runfolder.KindSuite, runfolder.ResultPassed, 1000, 2000)
	worker := done("w1", runfolder.KindSuite, runfolder.ResultPassed, 1100, 1900)
	worker.ParentRunID = "p1"
	worker.TestKey = "a.yml"
	parent.TestKey = "parent.yml"
	records, err := Collect(fakeSource{metas: []runfolder.Meta{parent, worker}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keysOf(records), []string{"parent.yml=passed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %v, want %v", got, want)
	}
}

func TestCollectKeepsDuplicateKeysInsideOneSuite(t *testing.T) {
	suite := done("s1", runfolder.KindSuite, runfolder.ResultFailed, 1, 2)
	attempt := []runfolder.IndexAttempt{{Number: 1}}
	source := fakeSource{
		metas: []runfolder.Meta{suite},
		indexes: map[string]runfolder.Index{"s1": {Entries: []runfolder.IndexEntry{
			{Path: "a.yml", TestKey: "k", Status: runfolder.ResultPassed, Attempts: attempt},
			{Path: "b.yml", TestKey: "k", Status: runfolder.ResultFailed, Attempts: attempt},
		}}},
	}
	records, err := Collect(source)
	if err != nil {
		t.Fatal(err)
	}
	stats := Aggregate(records, Window{})
	if len(stats) != 1 || stats[0].Runs != 2 || stats[0].Passed != 1 || stats[0].Failed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if got := stats[0].LastOutcomes; got[0] != OutcomeFailed || got[1] != OutcomePassed {
		t.Fatalf("last outcomes = %v, want the later entry first", got)
	}
}
