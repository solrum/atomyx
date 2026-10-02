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
// never finished, and a reader reports it as result cancelled with
// outcome interrupted. A legacy running record migrated into a folder
// therefore carries no completedAt, no result, and no outcome.
//
// Kind is script for a CLI run of one script, suite for a CLI suite or
// scenario run, and mcp for a run driven through the MCP server. A
// run_script call inside an open MCP run writes into that run. A bare
// run_script call with no open run is its own run of kind mcp.
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

// FromLegacyStatus maps the status of a flat legacy run record to the
// Result and Outcome of its run folder, and false for an unknown
// status. A legacy running record belongs to a run that never closed,
// so it maps to cancelled and interrupted.
func FromLegacyStatus(status string) (Result, Outcome, bool) {
	var o Outcome
	switch status {
	case "passed":
		o = OutcomePassed
	case "failed":
		o = OutcomeFailed
	case "error":
		o = OutcomeError
	case "cancelled":
		o = OutcomeCancelled
	case "running":
		o = OutcomeInterrupted
	default:
		return "", "", false
	}
	r, _ := ResultOf(o)
	return r, o, true
}
