package storage

import "dev.atomyx/internal/runfolder"

const (
	// RunKeyPrefix namespaces the flat run records in
	// Storage.
	RunKeyPrefix = runfolder.LegacyRecordKeyPrefix
	// BugKeyPrefix namespaces persisted bug records in Storage.
	BugKeyPrefix = "bugs/"
)

// RunKey returns the Storage key a flat run record is loaded under.
func RunKey(id string) string { return RunKeyPrefix + id }

// BugKey returns the Storage key a persisted bug record is saved and
// loaded under. The bug's optional screenshot payload is a sibling
// key at BugKey(id) + "/screenshot".
func BugKey(id string) string { return BugKeyPrefix + id }

// PersistedRun is the on-disk shape of a flat run record: RunRecord
// plus DurationMs and Summary. It is what RunKey(id) holds in Storage.
// Nothing writes new ones, and LegacyRunReader reads the ones that
// exist.
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
