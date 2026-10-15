package history

import (
	"math"
	"slices"
	"sort"
)

// LastOutcomesCap is the most outcomes TestStats.LastOutcomes lists.
const LastOutcomesCap = 10

const (
	percentile50 = 0.50
	percentile95 = 0.95
)

// Window narrows which records of a test count. Zero values mean no
// limit. LastN keeps the most recent N records per test, and SinceMs drops
// records that started before that epoch millisecond. When both are set,
// the time cut applies first and LastN then trims what remains.
type Window struct {
	LastN   int
	SinceMs int64
}

// TestStats is the outcome history of one test key inside a window.
//
// Runs counts every record in the window. Passed includes flaky records,
// because a flaky record passed in the end, and Failed counts the rest.
// InfraFailures and TestFailures split Failed by who the failure is blamed
// on, so Failed is their sum, and a failed record with no class counts as a
// test failure. InfraFailureRate is InfraFailures divided by Runs.
// FlakyRate is Flaky divided by Runs. LastOutcomes lists up to
// LastOutcomesCap outcomes, most recent first. The duration percentiles
// use the nearest-rank method over records with a known duration and are
// zero when none has one.
type TestStats struct {
	Key              string
	Runs             int
	Passed           int
	Failed           int
	Flaky            int
	FlakyRate        float64
	InfraFailures    int
	TestFailures     int
	InfraFailureRate float64
	LastOutcomes     []Outcome
	DurationP50Ms    int64
	DurationP95Ms    int64
}

// Aggregate groups records by test key and computes the stats of each key
// inside window. The result is sorted by key and holds no entry for a key
// the window leaves empty.
func Aggregate(records []Record, window Window) []TestStats {
	groups := make(map[string][]Record)
	for _, r := range records {
		if window.SinceMs > 0 && r.StartedAtMs < window.SinceMs {
			continue
		}
		groups[r.Key] = append(groups[r.Key], r)
	}
	stats := make([]TestStats, 0, len(groups))
	for key, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			if group[i].StartedAtMs != group[j].StartedAtMs {
				return group[i].StartedAtMs > group[j].StartedAtMs
			}
			if group[i].RunID != group[j].RunID {
				return group[i].RunID > group[j].RunID
			}
			return group[i].EntryIndex > group[j].EntryIndex
		})
		if window.LastN > 0 && len(group) > window.LastN {
			group = group[:window.LastN]
		}
		stats = append(stats, statsOf(key, group))
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Key < stats[j].Key })
	return stats
}

// statsOf computes the stats of group, which is sorted most recent first.
func statsOf(key string, group []Record) TestStats {
	s := TestStats{Key: key, Runs: len(group)}
	var durations []int64
	for i, r := range group {
		switch r.Outcome {
		case OutcomeFailed:
			s.Failed++
			if r.Class == ClassInfra {
				s.InfraFailures++
			} else {
				s.TestFailures++
			}
		case OutcomeFlaky:
			s.Flaky++
			s.Passed++
		default:
			s.Passed++
		}
		if i < LastOutcomesCap {
			s.LastOutcomes = append(s.LastOutcomes, r.Outcome)
		}
		if r.HasDuration {
			durations = append(durations, r.DurationMs)
		}
	}
	s.FlakyRate = float64(s.Flaky) / float64(s.Runs)
	s.InfraFailureRate = float64(s.InfraFailures) / float64(s.Runs)
	slices.Sort(durations)
	s.DurationP50Ms = nearestRank(durations, percentile50)
	s.DurationP95Ms = nearestRank(durations, percentile95)
	return s
}

// nearestRank returns the p-th percentile of sorted by the nearest-rank
// method, or zero for an empty sample.
func nearestRank(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}
