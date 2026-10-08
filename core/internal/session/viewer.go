package session

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

// ImageFormat names the encoding of a screenshot, read from its bytes.
type ImageFormat string

const (
	// ImageUnknown is reported when the bytes are not a PNG or a JPEG,
	// or when no screenshot has been captured.
	ImageUnknown ImageFormat = ""
	ImagePNG     ImageFormat = "png"
	ImageJPEG    ImageFormat = "jpeg"
)

// ObservationKind says which capture a notification announces.
type ObservationKind int

const (
	ObservedTree ObservationKind = iota + 1
	ObservedScreenshot
	ObservedScreenSize
	// ObservedDropped is the last notification of a subscriber that left
	// too many unread. The channel closes right after it, and the
	// subscriber may subscribe again and read Latest.
	ObservedDropped
	// ObservedClosed is the last notification of a subscriber whose
	// session closed. The channel closes right after it, and
	// subscribing again fails.
	ObservedClosed
	// ObservedStep announces that step lines are available up to the
	// sequence number in Revision. Only a SubscribeSince subscriber
	// receives it, and it reads the lines with StepsSince.
	ObservedStep
	// ObservedStepGap is the first notice of a SubscribeSince subscriber
	// that needs step lines the hub no longer holds. Revision is the
	// sequence number of the oldest line the hub still holds, and every
	// line before it is lost to that subscriber.
	ObservedStepGap
	// ObservedState announces a holder state transition. State, Command
	// and RunID carry the new state, so a subscriber never has to ask for
	// it. Every subscriber receives it, in order with the other notices.
	ObservedState
)

// Observation announces that a new capture is available. A capture
// notification carries no payload: a subscriber reads the value with
// Latest, so a slow reader only ever sees the newest capture and never a
// backlog. Only ObservedState carries one.
type Observation struct {
	Kind  ObservationKind
	Epoch uint64
	// Revision is the revision of the announced capture, the same value
	// the next Latest carries as TreeRevision or ScreenshotRevision. A
	// screen size has no revision in a Snapshot, so its count only
	// orders sizes.
	Revision uint64
	// Stable is false when a mutating call overlapped the capture.
	Stable bool
	// State, Command and RunID describe the holder after an ObservedState
	// transition and are empty for every other kind. RunID is empty while
	// no run is open.
	State   HolderState
	Command string
	RunID   string
	// Pause is the location of a paused run. It is set only on an
	// ObservedState whose State is StatePaused.
	Pause *PauseLocation
	// DroppedInterventions counts the intervention lines the run folder could
	// not keep. It is set on an ObservedState and is zero when none were lost.
	DroppedInterventions int
}

// Snapshot is the newest capture of each kind, as a viewer may show it.
// Callers read fields by name, so fields added later never break them.
// Every slice and map in it is shared and must be treated as read-only.
type Snapshot struct {
	// Tree is the newest hierarchy with every secret masked. It is the
	// raw masked tree, with every node the device reported, so a client
	// must not filter or prune it before it indexes nodes. A node path is
	// a position in this exact tree, and a path computed over a pruned
	// copy addresses a different node. It is the zero value while
	// HasTree is false.
	Tree    wire.TreeNodeWire
	HasTree bool
	// TreeEpoch is the action epoch the tree was fetched in. Several
	// trees fetched with no mutating call between them share an epoch,
	// so the epoch does not identify a tree.
	TreeEpoch uint64
	// TreeRevision identifies this stored tree. It increases with every
	// tree the hub stores, including trees of one epoch, and is zero
	// while HasTree is false. A node path, such as a child index chain,
	// is valid only against the revision of the tree it was read from, so
	// a client that sends a path back must send this revision with it.
	TreeRevision uint64
	// TreeStable reports that no mutating call overlapped the capture of
	// the newest tree. It is false while HasTree is false.
	TreeStable bool

	// Screenshot holds the encoded image of the newest screenshot.
	Screenshot    []byte
	HasScreenshot bool
	// ScreenshotFormat is read from the image bytes.
	ScreenshotFormat ImageFormat
	// ScreenshotEpoch is the action epoch the screenshot was fetched in.
	ScreenshotEpoch uint64
	// ScreenshotRevision identifies this stored screenshot the way
	// TreeRevision identifies a tree. It is zero while HasScreenshot is
	// false.
	ScreenshotRevision uint64
	// ScreenshotStable is TreeStable for the newest screenshot.
	ScreenshotStable bool

	// Scale is the screenshot pixel width divided by the screen width
	// the driver reports. It is zero while either is unknown.
	Scale float64
	// Paired reports that the tree and the screenshot were fetched in
	// the same action epoch and that no mutating call overlapped either
	// capture, so they describe the same screen.
	Paired bool
	// Highlightable reports that a tree node's bounds can be drawn on
	// the screenshot: the pair matches and a scale is known.
	Highlightable bool

	// Generation names the held session the snapshot came from. A
	// Session leaves it zero. A host that opens a new session after a
	// release or a loss stamps its own count, because every session
	// numbers its revisions from one, so a revision repeats across
	// sessions and only the pair of generation and revision identifies
	// a capture.
	Generation uint64
}

// HolderState is where a device holder stands in the holder state
// machine. It is the shared vocabulary of every layer that reports a
// holder, so a viewer reads one set of values whoever holds the device.
type HolderState string

const (
	// StateIdle: the device is held, no tool call is in flight and no run
	// is open. Only a host that guards the activity gate, the MCP server
	// or the inspector, ever marks a session idle.
	StateIdle HolderState = "idle"
	// StateRunning: the device is held and a call is in flight or a run is
	// open. A freshly opened session starts here, so a holder that never
	// calls SetIdle reports running for its whole life.
	StateRunning HolderState = "running"
	// StateReleased: the holder gave the device up after an idle timeout
	// and remembers which device it had. A Session never reports it,
	// because a released holder has no Session, so only the host that
	// owns the lifecycle does.
	StateReleased HolderState = "released"
	// StateBorrowed: the holder handed the device to another process and
	// keeps only a viewer connection. A Session never reports it, for the
	// same reason as StateReleased.
	StateBorrowed HolderState = "borrowed"
	// StatePaused: the holder's run is stopped at a step for a person to
	// debug. Only a session whose host injected a Debugger ever reports it.
	// It is never written to the lease metadata, which keeps reporting the
	// activity underneath.
	StatePaused HolderState = "paused"
	// StateLost: the holder lost the device without choosing to.
	StateLost HolderState = "lost"
	// StateClosed: the session was closed by its owner.
	StateClosed HolderState = "closed"
)

// Info describes who holds the device and what they are doing. State is
// the session's own state, set by its owner through SetRunning and
// SetIdle, so it is the same with or without a lease.
type Info struct {
	Device device.Descriptor `json:"device"`
	Holder lease.Holder      `json:"holder"`
	State  HolderState       `json:"state"`
	// Pause is where the run stands while State is StatePaused, and nil
	// otherwise.
	Pause *PauseLocation `json:"pause,omitempty"`
	// Generation is the host's count of sessions opened so far, as for
	// Snapshot.Generation. A Session leaves it zero.
	Generation uint64 `json:"generation"`
}

// Viewer reads what a session has captured. It never calls the device,
// so any number of viewers cost the holder nothing.
type Viewer interface {
	// Info reports the device, the holder and its state.
	Info() Info
	// Latest returns the newest capture of each kind. The tree has its
	// secrets masked.
	Latest() Snapshot
	// Subscribe reports every new capture until ctx ends, the session
	// closes, or the subscriber falls behind. The last value is
	// ObservedClosed or ObservedDropped, then the channel closes. Ending
	// through ctx sends no terminal value.
	Subscribe(ctx context.Context) (<-chan Observation, error)
}

// StepSource is a Viewer that also follows the step lines of the runs the
// session drives. A line carries text from the step stream, so it is
// read with StepsSince and never travels inside an Observation.
type StepSource interface {
	Viewer
	// StepsSince returns the held step lines after line sinceSeq of run
	// runID, oldest first, and reports whether lines were lost. Sequence
	// numbers restart with every run, so a caller that last read another
	// run, or none, passes its own runID and gets every held line of the
	// current run. A sinceSeq beyond the newest line of the same run is
	// an error.
	StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error)
	// SubscribeSince is Subscribe plus ObservedStep and ObservedStepGap
	// notices, resuming after line sinceSeq of run runID.
	SubscribeSince(ctx context.Context, runID string, sinceSeq int64) (<-chan Observation, error)
}

// Refresher captures a fresh tree and screenshot from the device.
type Refresher interface {
	// FreshTree captures a tree and a screenshot back to back, with no
	// tool call between them, and returns what a viewer would now read
	// from Latest. The pair is paired and highlightable when the
	// device reports its screen size. It never waits: while any tool
	// call is inside the activity gate, or another fresh capture holds
	// it, it returns a diag.CodeHolderBusy error at once, because a
	// capture taken beside a running call would interleave with it.
	FreshTree(ctx context.Context) (Snapshot, error)
}

// Controller is a Viewer that can also ask the device for a matched
// pair of captures. Only the process that holds the device lease has
// one.
type Controller interface {
	Viewer
	Refresher
	// Gate reports the activity gate that guards FreshTree. Only a
	// lease-holding local session has one, so a remote handle cannot
	// satisfy Controller.
	Gate() *ActivityGate
}
