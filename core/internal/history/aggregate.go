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
// InfraFailures and TestFailures split the failures by who they are blamed
// on. A failed record with no class counts as a test failure. AbortedRuns
// counts the runs that ended device lost or interrupted. They are not in
// Runs, Passed, Failed, or LastOutcomes, but each counts as an
// infrastructure failure, so Failed plus AbortedRuns equals InfraFailures
// plus TestFailures. Attempts is Runs plus AbortedRuns, and
// InfraFailureRate is InfraFailures divided by Attempts.
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
	AbortedRuns      int
	Attempts         int
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
		finished, aborted := splitAborted(group)
		finished, aborted = trimToLast(finished, aborted, window.LastN)
		stats = append(stats, statsOf(key, finished, aborted))
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Key < stats[j].Key })
	return stats
}

// statsOf computes the stats of group, which is sorted most recent first.
func statsOf(key string, group []Record, aborted []Record) TestStats {
	s := TestStats{Key: key, Runs: len(group), AbortedRuns: len(aborted)}
	s.InfraFailures = len(aborted)
	s.Attempts = s.Runs + s.AbortedRuns
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
	if s.Runs > 0 {
		s.FlakyRate = float64(s.Flaky) / float64(s.Runs)
	}
	if s.Attempts > 0 {
		s.InfraFailureRate = float64(s.InfraFailures) / float64(s.Attempts)
	}
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

// splitAborted separates the records of runs that were aborted by the
// infrastructure from the finished ones, keeping the order of group.
func splitAborted(group []Record) (finished, aborted []Record) {
	for _, r := range group {
		if r.Aborted {
			aborted = append(aborted, r)
		} else {
			finished = append(finished, r)
		}
	}
	return finished, aborted
}

// trimToLast keeps the most recent n finished records, and the aborted
// records that started at or after the oldest kept finished record, so
// the cut by count never changes which finished runs are reported. With
// no finished record to anchor on, it keeps the most recent n aborted
// ones. Both inputs are sorted most recent first, and n of zero keeps all.
func trimToLast(finished, aborted []Record, n int) ([]Record, []Record) {
	if n <= 0 {
		return finished, aborted
	}
	if len(finished) == 0 {
		return finished, aborted[:min(n, len(aborted))]
	}
	if len(finished) > n {
		finished = finished[:n]
	}
	cutoff := finished[len(finished)-1].StartedAtMs
	kept := aborted[:0:0]
	for _, r := range aborted {
		if r.StartedAtMs >= cutoff {
			kept = append(kept, r)
		}
	}
	return finished, kept
}
