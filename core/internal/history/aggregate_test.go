package history

import (
	"fmt"
	"reflect"
	"testing"
)

func rec(key string, at int64, outcome Outcome, durationMs int64) Record {
	return Record{Key: key, RunID: fmt.Sprintf("run-%04d", at), StartedAtMs: at, Outcome: outcome, DurationMs: durationMs, HasDuration: durationMs >= 0}
}

func TestAggregateEmptyInput(t *testing.T) {
	if got := Aggregate(nil, Window{}); len(got) != 0 {
		t.Fatalf("Aggregate(nil) = %+v, want empty", got)
	}
}

func TestAggregateCountsPassedFailedAndFlaky(t *testing.T) {
	records := []Record{
		rec("a.yml", 1, OutcomePassed, 100),
		rec("a.yml", 2, OutcomeFailed, 200),
		rec("a.yml", 3, OutcomeFlaky, 300),
		rec("a.yml", 4, OutcomePassed, 400),
	}
	got := Aggregate(records, Window{})
	if len(got) != 1 {
		t.Fatalf("got %d stats, want 1", len(got))
	}
	s := got[0]
	if s.Runs != 4 || s.Passed != 3 || s.Failed != 1 || s.Flaky != 1 || s.FlakyRate != 0.25 {
		t.Fatalf("stats = %+v", s)
	}
	wantLast := []Outcome{OutcomePassed, OutcomeFlaky, OutcomeFailed, OutcomePassed}
	if !reflect.DeepEqual(s.LastOutcomes, wantLast) {
		t.Fatalf("LastOutcomes = %v, want %v", s.LastOutcomes, wantLast)
	}
}

func TestAggregateSortsByKey(t *testing.T) {
	got := Aggregate([]Record{rec("b", 1, OutcomePassed, 1), rec("a", 1, OutcomePassed, 1)}, Window{})
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("order = %+v", got)
	}
}

func TestAggregateWindowCuts(t *testing.T) {
	var records []Record
	for i := int64(1); i <= 6; i++ {
		outcome := OutcomePassed
		if i <= 2 {
			outcome = OutcomeFailed
		}
		records = append(records, rec("a", i*1000, outcome, i))
	}
	tests := []struct {
		name       string
		window     Window
		wantRuns   int
		wantFailed int
	}{
		{"no window", Window{}, 6, 2},
		{"last 3 drops the old failures", Window{LastN: 3}, 3, 0},
		{"last larger than history", Window{LastN: 50}, 6, 2},
		{"since cuts older records", Window{SinceMs: 2000}, 5, 1},
		{"since after everything leaves no key", Window{SinceMs: 99999}, 0, 0},
		{"since then last", Window{SinceMs: 2000, LastN: 2}, 2, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Aggregate(records, tc.window)
			if tc.wantRuns == 0 {
				if len(got) != 0 {
					t.Fatalf("got %+v, want none", got)
				}
				return
			}
			if got[0].Runs != tc.wantRuns || got[0].Failed != tc.wantFailed {
				t.Fatalf("stats = %+v", got[0])
			}
		})
	}
}

func TestAggregateLastOutcomesIsCappedAndMostRecentFirst(t *testing.T) {
	var records []Record
	for i := int64(1); i <= 15; i++ {
		outcome := OutcomePassed
		if i == 15 {
			outcome = OutcomeFailed
		}
		records = append(records, rec("a", i, outcome, 1))
	}
	s := Aggregate(records, Window{})[0]
	if len(s.LastOutcomes) != LastOutcomesCap || s.LastOutcomes[0] != OutcomeFailed {
		t.Fatalf("LastOutcomes = %v", s.LastOutcomes)
	}
}

func TestAggregatePercentilesOnSmallSamples(t *testing.T) {
	tests := []struct {
		name      string
		durations []int64
		p50, p95  int64
	}{
		{"one sample", []int64{40}, 40, 40},
		{"two samples", []int64{10, 90}, 10, 90},
		{"three samples", []int64{30, 10, 20}, 20, 30},
		{"twenty samples", seq(20), 10, 19},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var records []Record
			for i, d := range tc.durations {
				records = append(records, rec("a", int64(i+1), OutcomePassed, d))
			}
			s := Aggregate(records, Window{})[0]
			if s.DurationP50Ms != tc.p50 || s.DurationP95Ms != tc.p95 {
				t.Fatalf("p50 = %d p95 = %d, want %d and %d", s.DurationP50Ms, s.DurationP95Ms, tc.p50, tc.p95)
			}
		})
	}
}

func seq(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}

func TestAggregateSkipsUnknownDurationsInPercentiles(t *testing.T) {
	records := []Record{
		rec("a", 1, OutcomePassed, 50),
		{Key: "a", RunID: "r2", StartedAtMs: 2, Outcome: OutcomePassed},
	}
	s := Aggregate(records, Window{})[0]
	if s.Runs != 2 || s.DurationP50Ms != 50 || s.DurationP95Ms != 50 {
		t.Fatalf("stats = %+v", s)
	}
	none := Aggregate([]Record{{Key: "b", RunID: "r", StartedAtMs: 1, Outcome: OutcomePassed}}, Window{})[0]
	if none.DurationP50Ms != 0 || none.DurationP95Ms != 0 {
		t.Fatalf("no durations should give zero percentiles: %+v", none)
	}
}
