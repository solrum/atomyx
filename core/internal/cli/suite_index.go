package cli

import (
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
)

// suiteIndexEntries turns a suite result into the entries of index.json.
// It is the one place that reads the attempts and kept videos of a suite
// result, and the suite report takes its per-entry videos from the
// entries it returns, so the report and the index cannot disagree.
func suiteIndexEntries(result scenario.SuiteResult) []runfolder.IndexEntry {
	entries := make([]runfolder.IndexEntry, len(result.Entries))
	for i, entry := range result.Entries {
		attempts := entry.Attempts
		if len(attempts) == 0 {
			attempts = []scenario.SuiteAttempt{{Number: 1, Status: entry.Status}}
		}
		indexed := make([]runfolder.IndexAttempt, len(attempts))
		for j, attempt := range attempts {
			indexed[j] = runfolder.IndexAttempt{
				Number: attempt.Number,
				Result: indexResult(attempt.Status),
				Videos: attempt.Videos,
			}
		}
		entries[i] = runfolder.IndexEntry{
			Path:        entry.Path,
			Status:      indexResult(entry.Status),
			Flaky:       entry.Flaky,
			Quarantined: entry.Quarantined,
			Attempts:    indexed,
		}
	}
	return entries
}

func indexResult(status scenario.SuiteEntryStatus) runfolder.Result {
	if status == scenario.EntryPassed {
		return runfolder.ResultPassed
	}
	return runfolder.ResultFailed
}

// indexEntryVideos returns the kept video files of each entry across all
// of its attempts in order, indexed like entries.
func indexEntryVideos(entries []runfolder.IndexEntry) [][]string {
	out := make([][]string, len(entries))
	for i, entry := range entries {
		for _, attempt := range entry.Attempts {
			out[i] = append(out[i], attempt.Videos...)
		}
	}
	return out
}

// suiteMetaCounts sets the flaky and quarantined counts of a suite result
// on the run's meta. A zero count stays absent from meta.json.
func suiteMetaCounts(result scenario.SuiteResult) func(*runfolder.Meta) {
	return func(m *runfolder.Meta) {
		m.FlakyEntries = result.Flaky
		m.QuarantinedFailures = result.QuarantinedFailures
	}
}
