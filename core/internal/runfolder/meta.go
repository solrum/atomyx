package runfolder

// SchemaVersion is the meta.json schema version this package writes
// and the highest one it reads.
const SchemaVersion = 1

// Kind says what started a run.
type Kind string

const (
	KindScript Kind = "script"
	KindSuite  Kind = "suite"
	KindMCP    Kind = "mcp"
)

// Result is the coarse verdict of a run. It is limited to three values
// so a reader that knows nothing about Outcome still gets a verdict.
type Result string

const (
	ResultPassed    Result = "passed"
	ResultFailed    Result = "failed"
	ResultCancelled Result = "cancelled"
)

// Outcome is the precise reason a run ended. Every Outcome belongs to
// exactly one Result, so a reader that only knows Result still gets a
// correct verdict.
type Outcome string

const (
	OutcomePassed      Outcome = "passed"
	OutcomeFailed      Outcome = "failed"
	OutcomeError       Outcome = "error"
	OutcomeCancelled   Outcome = "cancelled"
	OutcomeDeviceLost  Outcome = "deviceLost"
	OutcomeInterrupted Outcome = "interrupted"
)

// Kinds, Results, and Outcomes list every legal value in a stable order.
// The JSON Schema enums are built from them.
func Kinds() []Kind { return []Kind{KindScript, KindSuite, KindMCP} }

func Results() []Result {
	return []Result{ResultPassed, ResultFailed, ResultCancelled}
}

func Outcomes() []Outcome {
	return []Outcome{
		OutcomePassed, OutcomeFailed, OutcomeError,
		OutcomeCancelled, OutcomeDeviceLost, OutcomeInterrupted,
	}
}

// Device identifies the device a run drove.
type Device struct {
	Platform   string `json:"platform"`
	Kind       string `json:"kind,omitempty"`
	HardwareID string `json:"hardwareId,omitempty"`
	Name       string `json:"name,omitempty"`
}

// Host identifies the software that wrote the run.
type Host struct {
	AtomyxVersion string `json:"atomyxVersion,omitempty"`
	AgentVersion  string `json:"agentVersion,omitempty"`
}

// Finding is one bug finding recorded against an MCP run.
type Finding struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ScreenshotPath string `json:"screenshotPath,omitempty"`
	Timestamp      int64  `json:"timestamp"`
}

// MCPInfo carries the fields only an MCP-driven run has. A run with no
// findings omits Findings, and readers treat an absent list as empty.
// Summary is a pointer so an empty summary written by the run differs
// from a legacy record that never had one.
type MCPInfo struct {
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	ActionCount int       `json:"actionCount"`
	Findings    []Finding `json:"findings,omitempty"`
	Summary     *string   `json:"summary,omitempty"`
	DurationMs  int64     `json:"durationMs"`
}

// Meta is the content of meta.json.
//
// Result and Outcome are written only together with CompletedAt, in
// one atomic update. A meta without CompletedAt describes a run that
// has not finished and carries neither. Whether that run is still
// running or was interrupted depends on its writer, which Settle
// resolves through a Liveness. A legacy running record migrated into a
// folder carries no completedAt, no result, and no outcome.
//
// Kind is script for a CLI run of one script, suite for a CLI suite or
// scenario run, and mcp for a run driven through the MCP server. A
// run_script call inside an open MCP run writes into that run. A bare
// run_script call with no open run is its own run of kind mcp.
//
// ImportedFrom is set only on a folder ImportRun created from a record
// of another store, and holds that record's id. It lets a later import
// tell that the folder is the record's own, however the folder was
// edited since.
//
// FailedAtStep is the zero-based index of the step that failed, the
// same value the script runner reports as its failed step index.
// Display code adds one.
type Meta struct {
	SchemaVersion int      `json:"schemaVersion"`
	RunID         string   `json:"runId"`
	Kind          Kind     `json:"kind"`
	ScriptPath    string   `json:"scriptPath,omitempty"`
	ScriptName    string   `json:"scriptName,omitempty"`
	DeviceID      string   `json:"deviceId,omitempty"`
	Device        *Device  `json:"device,omitempty"`
	Host          *Host    `json:"host,omitempty"`
	StartedAt     int64    `json:"startedAt"`
	CompletedAt   *int64   `json:"completedAt,omitempty"`
	Result        Result   `json:"result,omitempty"`
	Outcome       Outcome  `json:"outcome,omitempty"`
	FailedAtStep  *int     `json:"failedAtStep,omitempty"`
	TotalSteps    int      `json:"totalSteps,omitempty"`
	MCP           *MCPInfo `json:"mcp,omitempty"`
	ImportedFrom  string   `json:"importedFrom,omitempty"`
	// InterventionsDropped counts the intervention lines that did not fit in
	// interventions.jsonl. Absent when none were dropped.
	InterventionsDropped int `json:"interventionsDropped,omitempty"`
	// Recording describes the screen recording of the run. Absent when
	// recording was off or the video was discarded.
	Recording *Recording `json:"recording,omitempty"`
	// FlakyEntries counts the suite entries that failed and then passed on
	// a retry. Absent when none did, and on a run that is not a suite.
	FlakyEntries int `json:"flakyEntries,omitempty"`
	// QuarantinedFailures counts the quarantined suite entries that failed.
	// They do not fail the suite. Absent when none did, and on a run that
	// is not a suite.
	QuarantinedFailures int `json:"quarantinedFailures,omitempty"`
}

// RecordingState is how a run's screen recording ended.
type RecordingState string

const (
	// RecordingKept means the video files are in the artifacts directory.
	RecordingKept RecordingState = "kept"
	// RecordingUnsupported means the device cannot record its screen.
	RecordingUnsupported RecordingState = "unsupported"
	// RecordingError means recording or keeping the video failed.
	RecordingError RecordingState = "error"
)

// Recording is the recording object of meta.json. Files are names
// relative to the artifacts directory, oldest first, and are present
// only when State is kept. Code is the stable error code and is present
// only when State is unsupported or error.
type Recording struct {
	State RecordingState `json:"state"`
	Files []string       `json:"files,omitempty"`
	Code  string         `json:"code,omitempty"`
}

// ResultOf returns the Result an Outcome belongs to, and false for a
// value outside the Outcome enum.
func ResultOf(o Outcome) (Result, bool) {
	switch o {
	case OutcomePassed:
		return ResultPassed, true
	case OutcomeFailed, OutcomeError:
		return ResultFailed, true
	case OutcomeCancelled, OutcomeDeviceLost, OutcomeInterrupted:
		return ResultCancelled, true
	}
	return "", false
}

// Legacy status strings of the flat run record and of the status field
// that list_runs and get_run still report.
const (
	legacyStatusPassed    = "passed"
	legacyStatusFailed    = "failed"
	legacyStatusError     = "error"
	legacyStatusCancelled = "cancelled"
	legacyStatusRunning   = "running"
)

// FromLegacyStatus maps the status of a flat legacy run record to the
// Result and Outcome of its run folder, and false for an unknown
// status. A legacy running record has not completed, so it maps to an
// empty Result and Outcome, the same shape as an unfinished meta, and
// Meta.Settle decides whether it is still running or interrupted.
func FromLegacyStatus(status string) (Result, Outcome, bool) {
	var o Outcome
	switch status {
	case legacyStatusPassed:
		o = OutcomePassed
	case legacyStatusFailed:
		o = OutcomeFailed
	case legacyStatusError:
		o = OutcomeError
	case legacyStatusCancelled:
		o = OutcomeCancelled
	case legacyStatusRunning:
		return "", "", true
	default:
		return "", "", false
	}
	r, _ := ResultOf(o)
	return r, o, true
}

// LegacyStatusOf is the inverse of FromLegacyStatus for a settled meta.
// A meta without a Result is a live run and reports running. A meta
// with a Result reports passed, failed, error, or cancelled, where
// error is the one outcome that shares a result with failed.
func LegacyStatusOf(m Meta) string {
	switch m.Outcome {
	case OutcomePassed:
		return legacyStatusPassed
	case OutcomeFailed:
		return legacyStatusFailed
	case OutcomeError:
		return legacyStatusError
	case OutcomeCancelled, OutcomeDeviceLost, OutcomeInterrupted:
		return legacyStatusCancelled
	}
	switch m.Result {
	case ResultPassed:
		return legacyStatusPassed
	case ResultFailed:
		return legacyStatusFailed
	case ResultCancelled:
		return legacyStatusCancelled
	}
	return legacyStatusRunning
}

// Liveness reports whether a run still has a live writer. A run with a
// live writer is running, and a run without one that never completed
// was interrupted. Implementations answer from whatever signal they
// hold, such as the in-process active run, and a composite that
// consults several signals satisfies the same contract.
type Liveness interface {
	IsLive(runID string) bool
}

// Settle resolves the verdict of a meta that has no CompletedAt. A run
// the Liveness reports as live carries no Result and no Outcome. Any
// other unfinished run carries result cancelled and outcome
// interrupted. A nil Liveness reports nothing as live. A meta with a
// CompletedAt is returned unchanged.
func (m Meta) Settle(l Liveness) Meta {
	if m.CompletedAt != nil {
		return m
	}
	if l != nil && l.IsLive(m.RunID) {
		m.Result, m.Outcome = "", ""
		return m
	}
	m.Result, m.Outcome = ResultCancelled, OutcomeInterrupted
	return m
}
