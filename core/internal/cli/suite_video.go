package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/scenario"
)

// suiteVideos records the screen once per suite entry and remembers the
// files each entry kept. Files are named from the entry's position, so
// two entries never collide, and the suite's own meta carries no
// recording field.
type suiteVideos struct {
	lifecycle *recording.Lifecycle
	desc      device.Descriptor
	art       recording.Artifacts

	mu   sync.Mutex
	kept map[int][]string
}

func newSuiteVideos(lifecycle *recording.Lifecycle, desc device.Descriptor, art recording.Artifacts) *suiteVideos {
	return &suiteVideos{lifecycle: lifecycle, desc: desc, art: art, kept: map[int][]string{}}
}

// entryVideoBase names the video files of one suite entry from its
// one-based position, for example video-entry-2-1.mp4.
func entryVideoBase(target scenario.EntryTarget) string {
	return fmt.Sprintf("entry-%d", target.EntryIndex+1)
}

// start begins the recording of one entry and returns the function that
// settles it with the entry's result. A failed entry keeps its video.
// A passed entry, or an entry cut short because the suite context ended
// (a stop signal or a lost device), discards it.
func (s *suiteVideos) start(ctx context.Context, target scenario.EntryTarget) scenario.EntryEnd {
	capture := s.lifecycle.Begin(ctx, s.desc, s.art, entryVideoBase(target))
	return func(res scenario.SuiteEntryResult) {
		result := runfolder.ResultPassed
		if res.Status != scenario.EntryPassed {
			result = runfolder.ResultFailed
			if ctx.Err() != nil {
				result = runfolder.ResultCancelled
			}
		}
		rec := capture.Finish(result) //nolint:contextcheck // cleanup runs under its own bounded context so an interrupted suite still stops the device recording
		if rec == nil || rec.State != runfolder.RecordingKept {
			return
		}
		s.mu.Lock()
		s.kept[target.EntryIndex] = rec.Files
		s.mu.Unlock()
	}
}

// entryFiles returns the kept video files of each of count entries,
// indexed by entry position.
func (s *suiteVideos) entryFiles(count int) [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]string, count)
	for i := range out {
		out[i] = s.kept[i]
	}
	return out
}

// printVideoLocations tells the operator where the kept video files of
// each entry are.
func (s *suiteVideos) printVideoLocations(stderr io.Writer, runDir string, count int) {
	for _, files := range s.entryFiles(count) {
		for _, name := range files {
			_, _ = fmt.Fprintf(stderr, "Video: %s\n", filepath.Join(runfolder.ArtifactsDir(runDir), name))
		}
	}
}
