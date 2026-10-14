package history

import "sort"

// Filter selects which tests and which part of their history a Query
// reports. Zero values mean no limit.
type Filter struct {
	// Key keeps only the test with exactly this key.
	Key string
	// Window narrows the records of every test.
	Window Window
}

// Result is the outcome of a Query. Tests holds one entry per test key,
// flakiest first, and is never nil. Skipped counts the run records the
// source could not read, so a caller can tell a partial view from a
// complete one.
type Result struct {
	Tests   []TestStats
	Skipped int
}

// Query reads every finished test run from source and returns the
// statistics of the tests filter selects.
//
// Tests are ordered by flaky rate, then by failures, both descending,
// then by key, so the order is deterministic. A source error is returned
// unchanged and the Result is then zero.
func Query(source Source, filter Filter) (Result, error) {
	records, skipped, err := collect(source)
	if err != nil {
		return Result{}, err
	}
	stats := Aggregate(records, filter.Window)
	if filter.Key != "" {
		kept := stats[:0]
		for _, s := range stats {
			if s.Key == filter.Key {
				kept = append(kept, s)
			}
		}
		stats = kept
	}
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.FlakyRate != b.FlakyRate {
			return a.FlakyRate > b.FlakyRate
		}
		if a.Failed != b.Failed {
			return a.Failed > b.Failed
		}
		return a.Key < b.Key
	})
	if stats == nil {
		stats = []TestStats{}
	}
	return Result{Tests: stats, Skipped: skipped}, nil
}
