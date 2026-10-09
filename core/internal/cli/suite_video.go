package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// suiteVideos records the screen once per suite entry attempt. Files are
// named from the entry's position and the attempt, so two entries or two
// attempts never collide, and the suite's own meta carries no recording
// field.
type suiteVideos struct {
	lifecycle *recording.Lifecycle
	desc      device.Descriptor
	art       recording.Artifacts
}

func newSuiteVideos(lifecycle *recording.Lifecycle, desc device.Descriptor, art recording.Artifacts) *suiteVideos {
	return &suiteVideos{lifecycle: lifecycle, desc: desc, art: art}
}

// entryVideoBase names the video files of one suite entry attempt from
// the entry's one-based position, for example video-entry-2-1.mp4. The
// first attempt keeps that name, and a later attempt adds its number, for
// example video-entry-2-attempt-2-1.mp4.
func entryVideoBase(target scenario.EntryTarget) string {
	base := fmt.Sprintf("entry-%d", target.EntryIndex+1)
	if target.Attempt > 1 {
		base += fmt.Sprintf("-attempt-%d", target.Attempt)
	}
	return base
}

// start begins the recording of one entry attempt and returns the
// function that settles it with the attempt's result and reports the kept
// files. A failed attempt keeps its video. A passed attempt, an attempt
// cut short because the suite context ended (a stop signal), and an
// attempt that ended because the device was lost all discard it, since a
// lost device cannot be pulled from.
func (s *suiteVideos) start(ctx context.Context, target scenario.EntryTarget) scenario.EntryEnd {
	capture := s.lifecycle.Begin(ctx, s.desc, s.art, entryVideoBase(target))
	return func(res scenario.SuiteEntryResult) []string {
		result := runfolder.ResultPassed
		if res.Status != scenario.EntryPassed {
			result = runfolder.ResultFailed
			if ctx.Err() != nil || outcomeDeviceLost(res.Result) {
				result = runfolder.ResultCancelled
			}
		}
		rec := capture.Finish(result) //nolint:contextcheck // cleanup runs under its own bounded context so an interrupted suite still stops the device recording
		if rec == nil || rec.State != runfolder.RecordingKept {
			return nil
		}
		return rec.Files
	}
}

// outcomeDeviceLost reports whether a script, or any script of a
// scenario, aborted because the device was lost.
func outcomeDeviceLost(outcome *scenario.SuiteEntryOutcome) bool {
	if outcome == nil {
		return false
	}
	if outcome.Script != nil {
		return scriptDeviceLost(outcome.Script)
	}
	if outcome.Scenario != nil {
		for _, child := range outcome.Scenario.Scripts {
			if scriptDeviceLost(child.Result) {
				return true
			}
		}
	}
	return false
}

func scriptDeviceLost(result *runner.ScriptResult) bool {
	return result != nil && result.Abort != nil && result.Abort.Reason == runner.RunAbortDeviceLost
}

// entryVideoFiles returns the kept video files of each entry of result,
// across all of its attempts in order, indexed like result.Entries.
func entryVideoFiles(result scenario.SuiteResult) [][]string {
	out := make([][]string, len(result.Entries))
	for i, entry := range result.Entries {
		for _, attempt := range entry.Attempts {
			out[i] = append(out[i], attempt.Videos...)
		}
	}
	return out
}

// printVideoLocations tells the operator where the kept video files are.
func printVideoLocations(stderr io.Writer, runDir string, entryVideos [][]string) {
	for _, files := range entryVideos {
		for _, name := range files {
			_, _ = fmt.Fprintf(stderr, "Video: %s\n", filepath.Join(runfolder.ArtifactsDir(runDir), name))
		}
	}
}
