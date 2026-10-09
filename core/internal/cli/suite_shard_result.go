package cli

import (
	"slices"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/shard"
)

// unrunMessage is the error of an entry no worker ran because every worker
// stopped.
const unrunMessage = "not run: every worker stopped before it started"

// shardedEntries merges the outcome of a sharded run into the entries of
// the suite result and the entries of index.json, both in the order of
// paths. An entry no worker ran is a failed entry with no attempts, so the
// result, the report and the exit code all count it.
func shardedEntries(paths []string, outcome shard.Outcome) ([]scenario.SuiteEntryResult, []runfolder.IndexEntry) {
	entries := make([]scenario.SuiteEntryResult, 0, len(outcome.Entries)+len(outcome.Unrun))
	index := make([]runfolder.IndexEntry, 0, cap(entries))
	for _, path := range paths {
		if i := slices.IndexFunc(outcome.Entries, func(e shard.Entry) bool { return e.Path == path }); i >= 0 {
			entries = append(entries, suiteEntryFromWire(outcome.Entries[i]))
			index = append(index, indexEntryFromWire(outcome.Entries[i]))
			continue
		}
		if slices.Contains(outcome.Unrun, path) {
			message := unrunMessage
			entries = append(entries, scenario.SuiteEntryResult{Path: path, Kind: scenario.KindScript, Status: scenario.EntryFailed, Error: &message})
			index = append(index, runfolder.IndexEntry{Path: path, Status: runfolder.ResultFailed, Attempts: []runfolder.IndexAttempt{}})
		}
	}
	return entries, index
}

// suiteEntryFromWire rebuilds the suite entry result a worker reported. It
// carries no script result, because the worker keeps those in its own run
// folder.
func suiteEntryFromWire(entry shard.Entry) scenario.SuiteEntryResult {
	attempts := make([]scenario.SuiteAttempt, len(entry.Attempts))
	for i, attempt := range entry.Attempts {
		attempts[i] = scenario.SuiteAttempt{
			Number:     attempt.Number,
			Status:     scenario.SuiteEntryStatus(attempt.Status),
			Error:      attempt.Error,
			DurationMs: attempt.DurationMs,
			Videos:     attempt.Videos,
		}
	}
	return scenario.SuiteEntryResult{
		Path:        entry.Path,
		Kind:        scenario.SuiteEntryKind(entry.Kind),
		Status:      scenario.SuiteEntryStatus(entry.Status),
		Error:       entry.Error,
		DurationMs:  entry.DurationMs,
		Attempts:    attempts,
		Flaky:       entry.Flaky,
		Quarantined: entry.Quarantined,
	}
}

// indexEntryFromWire builds the index.json entry of a reported entry, with
// each attempt naming the device it ran on and the worker run folder that
// holds its artifacts.
func indexEntryFromWire(entry shard.Entry) runfolder.IndexEntry {
	attempts := make([]runfolder.IndexAttempt, len(entry.Attempts))
	for i, attempt := range entry.Attempts {
		attempts[i] = runfolder.IndexAttempt{
			Number:      attempt.Number,
			Result:      indexResult(scenario.SuiteEntryStatus(attempt.Status)),
			Videos:      attempt.Videos,
			DeviceID:    attempt.DeviceID,
			WorkerRunID: attempt.WorkerRunID,
		}
	}
	return runfolder.IndexEntry{
		Path:        entry.Path,
		Status:      indexResult(scenario.SuiteEntryStatus(entry.Status)),
		Flaky:       entry.Flaky,
		Quarantined: entry.Quarantined,
		Attempts:    attempts,
	}
}
