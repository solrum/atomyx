package device

import (
	"context"

	"dev.atomyx/internal/diag"
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
type ScreenRecorder interface {
	Start(ctx context.Context, desc Descriptor, dir, name string) (Recording, error)
}

// Recording is one running screen recording.
//
// Stop ends the recording and returns the paths of the kept segment
// files in dir, oldest first, and cleans everything the recording left on
// the device. A cancelled ctx still ends the recording and cleans the
// device, but the segment files are then not guaranteed to be playable.
// Discard ends the recording and removes every file it produced, on the
// device and in dir. Stop and Discard are each safe to call once, and a
// second call returns an error.
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
	return unsupportedRecorder{}.Start(ctx, desc, dir, name)
}

// unsupportedRecorder is the recorder for a device that cannot record,
// such as a physical iOS device.
type unsupportedRecorder struct{}

func (unsupportedRecorder) Start(context.Context, Descriptor, string, string) (Recording, error) {
	return nil, diag.NewRecordingUnsupported()
}
