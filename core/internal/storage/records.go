package storage

const (
	// RunKeyPrefix namespaces persisted run records in Storage.
	RunKeyPrefix = "runs/"
	// BugKeyPrefix namespaces persisted bug records in Storage.
	BugKeyPrefix = "bugs/"
)

// RunKey returns the Storage key a persisted run record is saved and
// loaded under.
func RunKey(id string) string { return RunKeyPrefix + id }

// BugKey returns the Storage key a persisted bug record is saved and
// loaded under. The bug's optional screenshot payload is a sibling
// key at BugKey(id) + "/screenshot".
func BugKey(id string) string { return BugKeyPrefix + id }

// PersistedRun is the on-disk shape of a run record: RunRecord plus
// the fields computed at persistence time (DurationMs) and the
// caller-supplied Summary. It is what RunKey(id) holds in Storage.
type PersistedRun struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	Status      RunStatus      `json:"status"`
	StartedAt   int64          `json:"startedAt"`
	FinishedAt  *int64         `json:"finishedAt,omitempty"`
	DurationMs  int64          `json:"durationMs"`
	ActionCount int            `json:"actionCount"`
	Findings    []RunBugReport `json:"findings"`
	Summary     *string        `json:"summary,omitempty"`
}

// NewPersistedRun builds the on-disk record for a finished (or
// force-errored) run. finishedAtFallback is used to compute
// DurationMs when run.FinishedAt is nil, matching the fallback to the
// current time a caller applies when persisting a run whose
// FinishedAt has not been set yet.
func NewPersistedRun(run *RunRecord, summary *string, finishedAtFallback int64) PersistedRun {
	finishedAt := finishedAtFallback
	if run.FinishedAt != nil {
		finishedAt = *run.FinishedAt
	}
	return PersistedRun{
		ID:          run.ID,
		Name:        run.Name,
		Source:      run.Source,
		Status:      run.Status,
		StartedAt:   run.StartedAt,
		FinishedAt:  run.FinishedAt,
		DurationMs:  finishedAt - run.StartedAt,
		ActionCount: run.ActionCount,
		Findings:    run.Findings,
		Summary:     summary,
	}
}

// PersistedBug is the on-disk shape of a bug record, saved separately
// from its run so consumers can index bugs directly without opening
// the full run record. It is what BugKey(id) holds in Storage.
type PersistedBug struct {
	ID             string  `json:"id"`
	RunID          string  `json:"runId"`
	RunName        string  `json:"runName,omitempty"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	ScreenshotPath *string `json:"screenshotPath,omitempty"`
	Timestamp      int64   `json:"timestamp"`
}
