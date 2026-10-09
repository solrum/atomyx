package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/shutdown"
)

// runVideoBase names the video files of a single run. The files are
// video-run-1, video-run-2, and so on, oldest first.
const runVideoBase = "run"

// videoEnabled reports whether a run records its screen. The --video
// flag turns recording on whatever the config says, and without the flag
// the config's video setting decides. Off is the default.
func videoEnabled(flags map[string]FlagValue, loaded *config.LoadedConfig) bool {
	if flagBool(flags, "--video") {
		return true
	}
	return loaded != nil && loaded.Config.VideoOnFailure()
}

// videoLifecycle returns the recording lifecycle of a command. It is off,
// and makes no recorder call, unless enabled is true and the command has
// a recorder.
func (d Deps) videoLifecycle(enabled bool) *recording.Lifecycle {
	if !enabled {
		return recording.NewLifecycle(nil, d.logger())
	}
	return recording.NewLifecycle(d.Recorder, d.logger())
}

// beginVideo starts recording the screen of desc into the run folder. The
// recording runs until finishVideo or end settles it.
func (r *runRecord) beginVideo(ctx context.Context, lifecycle *recording.Lifecycle, desc device.Descriptor) {
	r.capture = lifecycle.Begin(ctx, desc, r.writer, runVideoBase)
}

// settleOutcome turns an outcome other than passed into cancelled when a
// stop signal canceled ctx, because the runner reports a step interrupted
// by the signal as a failure the operator did not cause.
func (r *runRecord) settleOutcome(ctx context.Context) {
	if _, interrupted := shutdown.InterruptExitCode(ctx); interrupted && r.outcome != runfolder.OutcomePassed {
		r.outcome = runfolder.OutcomeCancelled
	}
}

// finishVideo ends the recording for the outcome recorded so far and
// returns the recording field of the run, nil when recording was off or
// the video was discarded. A failed run keeps its video. A passed,
// cancelled, or device-lost run discards it. It is safe to call more than
// once and returns the first answer.
func (r *runRecord) finishVideo(ctx context.Context) *runfolder.Recording {
	if r.capture == nil {
		return nil
	}
	r.settleOutcome(ctx)
	result, _ := runfolder.ResultOf(r.outcome)
	r.recording = r.capture.Finish(result) //nolint:contextcheck // cleanup runs under its own bounded context so an interrupted run still stops the device recording
	return r.recording
}

// jsonRecording is the recording field of the --json run output. Files
// are names relative to the artifacts directory of the run folder.
type jsonRecording struct {
	State string   `json:"state"`
	Files []string `json:"files,omitempty"`
	Code  string   `json:"code,omitempty"`
}

func toJSONRecording(rec *runfolder.Recording) *jsonRecording {
	if rec == nil {
		return nil
	}
	return &jsonRecording{State: string(rec.State), Files: rec.Files, Code: rec.Code}
}

func toReportRecording(rec *runfolder.Recording) *report.ReportRecording {
	if rec == nil {
		return nil
	}
	return &report.ReportRecording{State: string(rec.State), Files: rec.Files, Code: rec.Code}
}

// printVideoLocation tells the operator where the kept video files are.
func printVideoLocation(stderr io.Writer, record *runRecord, rec *runfolder.Recording) {
	if rec == nil || rec.State != runfolder.RecordingKept {
		return
	}
	for _, name := range rec.Files {
		_, _ = fmt.Fprintf(stderr, "Video: %s\n", filepath.Join(runfolder.ArtifactsDir(record.Dir()), name))
	}
}
