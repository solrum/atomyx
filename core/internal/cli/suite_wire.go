package cli

import (
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/shard"
)

// wireEntry turns a finished suite entry into the entry a worker reports.
// An entry built without attempts counts as one attempt carrying the
// entry's own outcome.
func wireEntry(entry scenario.SuiteEntryResult) shard.Entry {
	attempts := entry.Attempts
	if len(attempts) == 0 {
		attempts = []scenario.SuiteAttempt{{Number: 1, Status: entry.Status, Error: entry.Error, DurationMs: entry.DurationMs}}
	}
	wire := shard.Entry{
		Path:        entry.Path,
		Kind:        string(entry.Kind),
		Status:      string(entry.Status),
		Error:       entry.Error,
		DurationMs:  entry.DurationMs,
		Flaky:       entry.Flaky,
		Quarantined: entry.Quarantined,
		Attempts:    make([]shard.Attempt, len(attempts)),
	}
	for i, attempt := range attempts {
		wire.Attempts[i] = shard.Attempt{
			Number:     attempt.Number,
			Status:     string(attempt.Status),
			Error:      attempt.Error,
			DurationMs: attempt.DurationMs,
			Videos:     attempt.Videos,
		}
	}
	return wire
}
