package device

import (
	"context"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
)

// ScreenRecorder starts a screen recording on one device.
//
// Start begins recording the screen of desc and returns a handle that
// owns the recording until it is stopped or discarded. Recorded files are
// written into dir under dot-prefixed temporary names built from name, so
// a caller adopts the ones it keeps and nothing half-written is ever
// visible under a final name. A device that cannot record fails Start
// with an error carrying diag.CodeRecordingUnsupported.
//
// The recorder owns segmenting. A long recording is split into segments
// and only the most recent ones are kept, so the caller never sees a
// segment boundary and disk use stays bounded however long the run lasts.
// A segment that did not finish cleanly, or that failed to rotate in,
// is dropped and never returned, so every returned file plays.
type ScreenRecorder interface {
	Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error)
}

// Recording is one running screen recording.
//
// Stop ends the recording and returns the paths of the kept segment
// files in dir, oldest first, and cleans everything the recording left on
// the device. It fails only when no playable segment remains. A cancelled ctx still ends the recording and cleans the
// device, but it then fails and returns no files. Discard ends the
// recording if it is still running and removes every file it produced, on
// the device and in dir, including files an earlier Stop returned, so a
// caller decides whether to keep a video after it has stopped. A second
// Stop fails, and Stop after Discard fails. Discard is safe to repeat.
type Recording interface {
	Stop(ctx context.Context) ([]string, error)
	Discard(ctx context.Context) error
}

// RecorderDeps are the host tools the platform recorders drive.
type RecorderDeps struct {
	// Runner runs short host commands such as adb pulls.
	Runner process.Runner
	// Background starts the long-running recorder processes.
	Background process.BackgroundRunner
	// Logger receives recording problems that do not end the recording,
	// such as a segment rotation that failed and will be retried. Nil
	// discards them.
	Logger logging.Logger
}

func (d RecorderDeps) logger() logging.Logger {
	if d.Logger == nil {
		return logging.NoopLogger{}
	}
	return d.Logger
}

// NewScreenRecorder returns the ScreenRecorder that picks an
// implementation from the Descriptor passed to Start. A device kind with
// no recorder fails Start with diag.CodeRecordingUnsupported.
func NewScreenRecorder(deps RecorderDeps) ScreenRecorder {
	return platformRecorder{deps: deps}
}

type platformRecorder struct {
	deps RecorderDeps
}

func (r platformRecorder) Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error) {
	var recorder ScreenRecorder = unsupportedRecorder{}
	switch {
	case desc.Platform == PlatformAndroid:
		recorder = adbRecorder{deps: r.deps, wait: waitRotationInterval}
	case desc.Platform == PlatformIOS && desc.Kind == KindSimulator:
		recorder = simulatorRecorder{deps: r.deps, wait: waitRotationInterval}
	}
	return recorder.Start(ctx, desc, dir, name)
}

// unsupportedRecorder is the recorder for a device that cannot record,
// such as a physical iOS device.
type unsupportedRecorder struct{}

func (unsupportedRecorder) Start(context.Context, Descriptor, string, string) (Recording, error) {
	return nil, diag.NewRecordingUnsupported()
}
