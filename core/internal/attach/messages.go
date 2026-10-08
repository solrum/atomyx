package attach

import (
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

// Request types of the session group.
const (
	TypeHello  = "session.hello"
	TypeStatus = "session.status"
	TypeBye    = "session.bye"
)

// Request types of the observe group.
const (
	TypeSubscribe        = "observe.subscribe"
	TypeTreeLatest       = "observe.tree.latest"
	TypeScreenshotLatest = "observe.screenshot.latest"
	TypeTreeFresh        = "observe.tree.fresh"
)

// Request types of the control group.
const (
	TypeControlYield = "control.yield"
)

// Request types of the debug group, and act.step, which runs one step
// while the run is paused.
const (
	TypeDebugBreakpointsSet = "debug.breakpoints.set"
	TypeDebugPause          = "debug.pause"
	TypeDebugResume         = "debug.resume"
	TypeDebugStep           = "debug.step"
	TypeDebugVariablesGet   = "debug.variables.get"
	TypeActStep             = "act.step"
)

// Notice types that a subscription starts.
const (
	NoticeTree       = "observe.notice.tree"
	NoticeScreenshot = "observe.notice.screenshot"
	NoticeStep       = "observe.notice.step"
	NoticeStepGap    = "observe.notice.stepGap"
	NoticeClosed     = "observe.notice.closed"
	NoticeState      = "observe.notice.state"
)

// FeatureObserve is the feature name of the observe group in the
// feature list of a hello exchange.
const FeatureObserve = "observe"

// FeatureObserveState is the feature name of the holder state notice.
// A holder that advertises it sends observe.notice.state on every
// subscription, and a viewer that does not know the notice ignores it.
const FeatureObserveState = "observe.state"

// FeatureControl is the feature name of the control group. A holder
// advertises it only while it can yield the device, so a client that
// finds it absent must not send control.yield.
const FeatureControl = "control"

// FeatureDebug is the feature name of the debug group and act.step. A
// holder advertises it only while its host can pause the run, so a client
// that finds it absent must not send a debug or act request.
const FeatureDebug = "debug"

// VersionRange is an inclusive range of major protocol versions.
type VersionRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SupportedVersions is the range of major versions this build speaks.
var SupportedVersions = VersionRange{Min: ProtocolMajor, Max: ProtocolMajor}

// NegotiateVersion picks the highest version both ranges contain, or
// returns a diag.CodeUnsupportedVersion error when they do not overlap.
func NegotiateVersion(client, server VersionRange) (int, error) {
	chosen := min(client.Max, server.Max)
	if chosen < client.Min || chosen < server.Min || client.Min > client.Max {
		return 0, diag.NewUnsupportedVersion(client.Min, client.Max, server.Min, server.Max)
	}
	return chosen, nil
}

// MaxClientNameBytes bounds Hello.Client. A longer name is refused with
// diag.CodeInvalidArgument.
const MaxClientNameBytes = 128

// Hello is the data of session.hello, the first request of a connection.
type Hello struct {
	Protocol VersionRange `json:"protocol"`
	Client   string       `json:"client"`
	Features []string     `json:"features"`
}

// HelloResult answers session.hello with the chosen version and the
// features the holder serves.
type HelloResult struct {
	Protocol int      `json:"protocol"`
	Features []string `json:"features"`
}

// DeviceInfo names the device a holder drives.
type DeviceInfo struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Name     string `json:"name"`
}

// HolderInfo describes the process that holds the device. StartedAt is
// in epoch milliseconds.
type HolderInfo struct {
	PID       int    `json:"pid"`
	Command   string `json:"command"`
	Role      string `json:"role"`
	RunID     string `json:"runId,omitempty"`
	StartedAt int64  `json:"startedAt"`
}

// Status answers session.status. State holds the holder state name, for
// example idle or running. Generation counts the sessions the holder has
// opened, because revisions restart with each one.
type Status struct {
	State      string     `json:"state"`
	Device     DeviceInfo `json:"device"`
	Holder     HolderInfo `json:"holder"`
	Generation uint64     `json:"generation"`
}

// Subscribe is the data of observe.subscribe. A viewer that resumes sends
// the run id and the sequence number of the last step line it read, and
// the holder replays the lines it still holds above it. Sequence numbers
// restart with every run, so a RunID that is empty or is not the run the
// holder is on replays every held line of the current run. A SinceSeq
// beyond the newest line of the same run is refused with
// diag.CodeInvalidArgument. Zero with no RunID starts from the lines the
// holder holds now.
type Subscribe struct {
	RunID    string `json:"runId,omitempty"`
	SinceSeq int64  `json:"sinceSeq"`
}

// CaptureState says how far a capture can be trusted when a viewer
// draws it. Stable is false when a mutating call overlapped the announced
// or returned capture. Paired, Highlightable and Scale describe the
// holder's newest tree and screenshot together when the message was
// produced: Paired means they show the same screen, Highlightable means a
// node's bounds can be drawn on the screenshot, and Scale is zero while
// unknown.
type CaptureState struct {
	Stable        bool    `json:"stable"`
	Paired        bool    `json:"paired"`
	Highlightable bool    `json:"highlightable"`
	Scale         float64 `json:"scale"`
}

// TreeNotice announces a new tree. The viewer reads it with
// observe.tree.latest.
type TreeNotice struct {
	Revision uint64 `json:"revision"`
	Epoch    uint64 `json:"epoch"`
	CaptureState
}

// ScreenshotNotice announces a new screenshot. The viewer reads it with
// observe.screenshot.latest.
type ScreenshotNotice struct {
	Revision uint64 `json:"revision"`
	Epoch    uint64 `json:"epoch"`
	CaptureState
}

// StateNotice announces that the holder moved to State, one of the holder
// state names Status carries. Command is the holder's command line name
// and RunID is the open run, absent while none is open. Pause says where
// the run stands and is present only when State is paused.
type StateNotice struct {
	State   string         `json:"state"`
	Command string         `json:"command"`
	RunID   string         `json:"runId,omitempty"`
	Pause   *PauseLocation `json:"pause,omitempty"`
}

// PauseLocation says where a paused run stands. Source is the absolute
// path of the script file, or "inline" for a script passed as text. Line
// is the step's line, zero for a nested step that has none. Depth is the
// nesting depth, zero for a top-level step. Command is the command name
// of the step about to run.
type PauseLocation struct {
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Depth   int    `json:"depth"`
	Command string `json:"command"`
}

// StepLine is one line of a run's step stream. Its JSON is identical to
// one line of steps.jsonl.
type StepLine struct {
	SchemaVersion int      `json:"schemaVersion"`
	RunID         string   `json:"runId"`
	Seq           int64    `json:"seq"`
	StepIndex     int      `json:"stepIndex"`
	Command       string   `json:"command"`
	Phase         string   `json:"phase"`
	Ok            bool     `json:"ok"`
	DurationMs    int64    `json:"durationMs"`
	Detail        *string  `json:"detail"`
	Artifacts     []string `json:"artifacts"`
}

// StepNotice carries one step line and the action epoch it was
// published in.
type StepNotice struct {
	Epoch uint64   `json:"epoch"`
	Line  StepLine `json:"line"`
}

// StepGapNotice tells a resuming viewer that step lines below OldestSeq
// are lost to it.
type StepGapNotice struct {
	OldestSeq int64 `json:"oldestSeq"`
}

// TreeResult answers observe.tree.latest. The tree is masked. Tree is
// absent while the holder has captured none.
type TreeResult struct {
	Revision uint64             `json:"revision"`
	Epoch    uint64             `json:"epoch"`
	Tree     *wire.TreeNodeWire `json:"tree,omitempty"`
	CaptureState
}

// ScreenshotResult answers observe.screenshot.latest. Image holds the
// encoded bytes, which JSON carries as base64, and is absent while the
// holder has captured none. The image is not masked.
type ScreenshotResult struct {
	Revision uint64 `json:"revision"`
	Epoch    uint64 `json:"epoch"`
	Format   string `json:"format,omitempty"`
	Image    []byte `json:"image,omitempty"`
	CaptureState
}

// FreshResult answers observe.tree.fresh with a tree and a screenshot
// captured back to back. Paired and Highlightable follow the session
// snapshot of the same names, and Scale is zero while unknown.
type FreshResult struct {
	Tree          TreeResult       `json:"tree"`
	Screenshot    ScreenshotResult `json:"screenshot"`
	Paired        bool             `json:"paired"`
	Highlightable bool             `json:"highlightable"`
	Scale         float64          `json:"scale"`
}

// MaxBorrowerCommandBytes bounds ControlYield.Command. A longer one is
// refused with diag.CodeInvalidArgument.
const MaxBorrowerCommandBytes = 256

// ControlYield is the data of control.yield. Command is a short human
// readable name of the process that wants the device, for example
// "atomyx run login.yml", which the holder may show to its own user. The
// holder answers ok after it has committed to release the device, and
// closes the connection once that reply is written. It answers
// diag.CodeHolderBusy when it cannot release now, and the connection
// stays usable.
type ControlYield struct {
	Command string `json:"command"`
}

// DebugBreakpoints is the data of debug.breakpoints.set. Locations
// replaces every breakpoint, each written as "<script file>:<line>" or
// "inline:<line>", and the last writer wins. A location that matches no
// authored step is refused with diag.CodeInvalidArgument and leaves the
// earlier breakpoints in place. The request is admitted at any time while
// a run is open.
type DebugBreakpoints struct {
	Locations []string `json:"locations"`
}

// DebugResume is the data of debug.resume and debug.step. Force skips the
// advisory check of the next step. debug.step resumes and stops again
// after the next step.
type DebugResume struct {
	Force bool `json:"force,omitempty"`
}

// DebugResumeResult answers debug.resume and debug.step. Resumed is false
// when the advisory check refused to resume, and MissReason then says why
// the next step looks unable to run. The viewer may repeat the request
// with Force set.
type DebugResumeResult struct {
	Resumed    bool   `json:"resumed"`
	MissReason string `json:"missReason,omitempty"`
}

// DebugVariables answers debug.variables.get with the variables visible
// at the pause. Every value is already redacted by the holder.
type DebugVariables struct {
	Variables map[string]string `json:"variables"`
}

// ActStep is the data of act.step. Step is one step written in script
// YAML. A step that nests other steps is refused with
// diag.CodeInvalidArgument.
type ActStep struct {
	Step string `json:"step"`
}

// ActResult answers act.step. Outcome is "passed" or "failed", Message
// describes a failure, and Artifacts lists the run-relative files the
// step produced.
type ActResult struct {
	Command    string   `json:"command"`
	Outcome    string   `json:"outcome"`
	Message    string   `json:"message,omitempty"`
	DurationMs int64    `json:"durationMs"`
	Artifacts  []string `json:"artifacts,omitempty"`
}
