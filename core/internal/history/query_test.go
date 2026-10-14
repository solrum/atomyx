package history

import (
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func keyedRun(id, key string, result runfolder.Result, start int64) runfolder.Meta {
	m := done(id, runfolder.KindScript, result, start, start+10)
	m.TestKey = key
	return m
}

func queryKeys(r Result) []string {
	var keys []string
	for _, s := range r.Tests {
		keys = append(keys, s.Key)
	}
	return keys
}

func TestQueryOrdersFlakiestFirst(t *testing.T) {
	suite := done("s1", runfolder.KindSuite, runfolder.ResultPassed, 100, 200)
	source := fakeSource{
		metas: []runfolder.Meta{
			keyedRun("r1", "steady", runfolder.ResultPassed, 1),
			keyedRun("r2", "broken", runfolder.ResultFailed, 2),
			keyedRun("r3", "alpha", runfolder.ResultPassed, 3),
			suite,
		},
		indexes: map[string]runfolder.Index{"s1": {Entries: []runfolder.IndexEntry{
			{Path: "shaky", Status: runfolder.ResultPassed, Flaky: true, Attempts: []runfolder.IndexAttempt{{Number: 1}, {Number: 2}}},
		}}},
	}
	got, err := Query(source, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shaky", "broken", "alpha", "steady"}
	if keys := queryKeys(got); !reflect.DeepEqual(keys, want) {
		t.Fatalf("order = %v, want %v", keys, want)
	}
}

func TestQueryFiltersByKeyAndWindow(t *testing.T) {
	source := fakeSource{metas: []runfolder.Meta{
		keyedRun("r1", "a", runfolder.ResultFailed, 1000),
		keyedRun("r2", "a", runfolder.ResultPassed, 2000),
		keyedRun("r3", "a", runfolder.ResultPassed, 3000),
		keyedRun("r4", "b", runfolder.ResultPassed, 4000),
	}}
	tests := []struct {
		name     string
		filter   Filter
		wantKeys []string
		wantRuns int
	}{
		{"no filter", Filter{}, []string{"a", "b"}, 3},
		{"key only", Filter{Key: "a"}, []string{"a"}, 3},
		{"unknown key", Filter{Key: "zzz"}, nil, 0},
		{"last two", Filter{Key: "a", Window: Window{LastN: 2}}, []string{"a"}, 2},
		{"since", Filter{Key: "a", Window: Window{SinceMs: 2000}}, []string{"a"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Query(source, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if got.Tests == nil {
				t.Fatal("Tests must never be nil")
			}
			if keys := queryKeys(got); !reflect.DeepEqual(keys, tc.wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, tc.wantKeys)
			}
			if len(got.Tests) > 0 && got.Tests[0].Runs != tc.wantRuns {
				t.Fatalf("runs = %d, want %d", got.Tests[0].Runs, tc.wantRuns)
			}
		})
	}
}

func TestQueryCarriesSkippedCount(t *testing.T) {
	got, err := Query(fakeSource{skipped: 3}, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Skipped != 3 || got.Tests == nil || len(got.Tests) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func TestQueryPropagatesSourceError(t *testing.T) {
	boom := errors.New("boom")
	got, err := Query(fakeSource{err: boom}, Filter{})
	if !errors.Is(err, boom) || got.Skipped != 0 || got.Tests != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}
