package report

import (
	"dev.atomyx/internal/config"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// ReportCommand identifies which CLI command produced the run a
// report describes.
type ReportCommand string

const (
	ReportCommandRun   ReportCommand = "run"
	ReportCommandSuite ReportCommand = "suite"
)

// ReportKind identifies which runner produced a ReportInput's result:
// a standalone script, a standalone scenario, or a suite.
type ReportKind string

const (
	ReportKindScript   ReportKind = "script"
	ReportKindScenario ReportKind = "scenario"
	ReportKindSuite    ReportKind = "suite"
)

// ReportMetadata carries facts about a run that exist outside any
// individual script result: identity, timing, and the environment
// the run targeted. Every report format renders the same metadata.
type ReportMetadata struct {
	// RunID uniquely identifies this run.
	RunID      string        `json:"runId"`
	Label      string        `json:"label"`
	StartedAt  string        `json:"startedAt"`
	FinishedAt string        `json:"finishedAt"`
	DurationMs int64         `json:"durationMs"`
	Command    ReportCommand `json:"command"`
	// Env names the selected project config environment, when one
	// applied.
	Env      *string `json:"env,omitempty"`
	Platform *string `json:"platform,omitempty"`
	Device   *string `json:"device,omitempty"`
	// Abort is non-nil when the run it describes stopped early for a
	// reason beyond an ordinary step failure, such as the device or
	// agent going unreachable mid-run.
	Abort *ReportAbort `json:"abort,omitempty"`
}

// ReportAbort carries why a run stopped early for a reason beyond an
// ordinary step failure, for a report bundle's metadata.
type ReportAbort struct {
	Reason  string `json:"reason"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Remedy  string `json:"remedy,omitempty"`
}

// ReportScreenshot is one screenshot captured during a run, tagged
// with the scope of the result it belongs to.
//
// Scope identifies which script inside a result tree captured the
// screenshot, so a scope stays unique even when two runs in the same
// tree share an identical script path. RootScriptScope is the value
// every screenshot from a standalone script run carries, since there
// is only one script in the result and no position to key on.
//
// MimeType names the encoding of Data (for example "image/png" or
// "image/jpeg") and drives the file extension BuildScreenshotIndex
// assigns. An empty MimeType is treated as "image/png", matching
// every screenshot recorded before this field existed.
type ReportScreenshot struct {
	Scope    string
	Label    string
	Data     []byte
	MimeType string
}

// ReportInput is the port RenderJSON, RenderHTML, and a Reporter
// accept. Every report kind implements it: ScriptReportInput,
// ScenarioReportInput, and SuiteReportInput.
type ReportInput interface {
	ReportKind() ReportKind
	ReportMetadata() ReportMetadata
	ReportScreenshots() []ReportScreenshot
}

// ScriptReportInput is the "script" kind of ReportInput: the result
// of a single runner.ScriptRunner.Run call.
type ScriptReportInput struct {
	Metadata    ReportMetadata
	Result      runner.ScriptResult
	Screenshots []ReportScreenshot
}

func (i ScriptReportInput) ReportKind() ReportKind                { return ReportKindScript }
func (i ScriptReportInput) ReportMetadata() ReportMetadata        { return i.Metadata }
func (i ScriptReportInput) ReportScreenshots() []ReportScreenshot { return i.Screenshots }

// ScenarioReportInput is the "scenario" kind of ReportInput: the
// result of a single scenario.ScenarioRunner.Run call.
type ScenarioReportInput struct {
	Metadata    ReportMetadata
	Result      scenario.ScenarioResult
	Screenshots []ReportScreenshot
}

func (i ScenarioReportInput) ReportKind() ReportKind                { return ReportKindScenario }
func (i ScenarioReportInput) ReportMetadata() ReportMetadata        { return i.Metadata }
func (i ScenarioReportInput) ReportScreenshots() []ReportScreenshot { return i.Screenshots }

// SuiteReportInput is the "suite" kind of ReportInput: the result of
// a single scenario.SuiteRunner.Run call.
type SuiteReportInput struct {
	Metadata    ReportMetadata
	Result      scenario.SuiteResult
	Screenshots []ReportScreenshot
}

func (i SuiteReportInput) ReportKind() ReportKind                { return ReportKindSuite }
func (i SuiteReportInput) ReportMetadata() ReportMetadata        { return i.Metadata }
func (i SuiteReportInput) ReportScreenshots() []ReportScreenshot { return i.Screenshots }

// ReportOptions selects which formats a Reporter writes and the
// directories it writes into. The caller decides both directories, so
// the report package holds no knowledge of where a run lives on disk.
//
// Screenshots always go into ArtifactsDir. ReportDir is created only
// when Formats is not empty. Image links in the HTML report are
// computed from ReportDir to ArtifactsDir, so the two directories may
// sit anywhere relative to each other.
type ReportOptions struct {
	Formats      []string
	ReportDir    string
	ArtifactsDir string
}

// FormatHTML and FormatJSON are the two values a ReportOptions.Formats
// entry accepts, re-exported from config so callers that only import
// report do not also need config for these two literals.
const (
	FormatHTML = config.ReportFormatHTML
	FormatJSON = config.ReportFormatJSON
)

// ReportBundle is where a generated report landed, and every file the
// Reporter wrote, screenshots included.
type ReportBundle struct {
	BundleDir string // the report directory, as given in ReportOptions
	Files     []string
}
