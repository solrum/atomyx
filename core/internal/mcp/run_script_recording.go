package mcp

import (
	"context"
	"path/filepath"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
)

// scriptVideoBase names the video files of a run_script call that is its
// own run, for example video-run-1.mp4.
const scriptVideoBase = "run"

// scriptRun is what one run_script execution produced: the runner's
// result, and the screen recording of the run when the call was its own
// run and recording was on.
type scriptRun struct {
	runner.ScriptResult
	// Recording is the recording field written to the run's meta, nil
	// when recording was off or the video was discarded.
	Recording *runfolder.Recording
	// ArtifactsDir is the directory the kept video files are in.
	ArtifactsDir string
}

// beginScriptRecording starts the recording of a script that is its own
// run. It returns nil, and makes no recorder call, when recording is off
// or the device is unknown.
func (s *runSession) beginScriptRecording(ctx context.Context, dev *device.Descriptor, art recording.Artifacts) *recording.Capture {
	if s.recordings == nil || dev == nil {
		return nil
	}
	return s.recordings.Begin(ctx, *dev, art, scriptVideoBase)
}

// finishScriptRecording ends capture for a run that ended with outcome. A
// failed run keeps its video. A passed, device-lost, or cancelled run
// discards it, except that a run cancelled because the tool call's time
// budget elapsed keeps it, since a hang is when a video helps most. A run
// the caller canceled, the session closed, or the device lost never
// keeps one. Cleanup runs under its own bounded context, so a call whose
// context was already canceled still stops the device recording.
func finishScriptRecording(ctx context.Context, capture *recording.Capture, outcome runfolder.Outcome, result runner.ScriptResult) *runfolder.Recording {
	if capture == nil {
		return nil
	}
	res, _ := runfolder.ResultOf(outcome)
	if outcome == runfolder.OutcomeCancelled && toolBudgetExpired(ctx) && !scriptLostDevice(result) {
		res = runfolder.ResultFailed
	}
	return capture.Finish(res) //nolint:contextcheck // cleanup runs under its own bounded context so a canceled call still stops the device recording
}

// scriptLostDevice reports whether the script aborted because the device
// was lost, which leaves nothing to pull a video from.
func scriptLostDevice(result runner.ScriptResult) bool {
	return result.Abort != nil && result.Abort.Reason == runner.RunAbortDeviceLost
}

// recordingWire is the run_script result's shape for a screen recording.
// Files are absolute paths, oldest first, and are present only when State
// is kept.
type recordingWire struct {
	State string   `json:"state"`
	Files []string `json:"files,omitempty"`
	Code  string   `json:"code,omitempty"`
}

func toRecordingWire(rec *runfolder.Recording, artifactsDir string) *recordingWire {
	if rec == nil {
		return nil
	}
	files := make([]string, len(rec.Files))
	for i, name := range rec.Files {
		files[i] = filepath.Join(artifactsDir, name)
	}
	if len(files) == 0 {
		files = nil
	}
	return &recordingWire{State: string(rec.State), Files: files, Code: rec.Code}
}
