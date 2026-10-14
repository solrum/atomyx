package history

// Outcome is how one run of a test ended, as far as history counts it.
type Outcome string

const (
	// OutcomePassed is a run that passed on its first attempt.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed is a run that failed.
	OutcomeFailed Outcome = "failed"
	// OutcomeFlaky is a suite entry that failed and then passed on a retry.
	// It counts as passed and as flaky.
	OutcomeFlaky Outcome = "flaky"
)

// Record is one finished run of one test. A script run yields one record,
// and a suite run yields one record per index entry.
//
// DurationMs is meaningful only when HasDuration is true. A suite entry
// carries no duration of its own, so its record leaves it unknown and the
// percentiles skip it.
type Record struct {
	Key         string
	RunID       string
	StartedAtMs int64
	Outcome     Outcome
	DurationMs  int64
	HasDuration bool
	// EntryIndex is the position of the entry inside its suite index, and
	// zero for a script run. It orders records of one run deterministically.
	EntryIndex int
}
