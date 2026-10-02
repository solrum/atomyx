package cli

import (
	"os"
	"path/filepath"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
)

// liveArtifacts is a runner.ScriptArtifacts that also writes each
// screenshot into the run folder's artifacts directory the moment it
// is added, so a run that crashes or is killed keeps every screenshot
// taken before it stopped.
//
// File names come from report.BuildScreenshotIndex over the root
// script scope, so they match the names the report writer assigns when
// it rewrites the same screenshots at the end of the run and the report
// links stay valid. A failed early write is not fatal because the
// report writer repeats every write and reports its own failure.
//
// liveArtifacts is also the runner.EvidenceWriter of a single-script
// run, so failure bundle files land beside the screenshots under their
// own step-NNN names.
type liveArtifacts struct {
	dir         string
	screenshots []runner.Screenshot
}

func newLiveArtifacts(dir string) *liveArtifacts {
	return &liveArtifacts{dir: dir}
}

func (a *liveArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	a.screenshots = append(a.screenshots, runner.Screenshot{Label: label, Data: data, MimeType: mimeType})
	refs := report.BuildScreenshotIndex(toReportScreenshots(a.screenshots)).Refs
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(a.dir, refs[len(refs)-1].File), data, 0o644)
}

func (a *liveArtifacts) Screenshots() []runner.Screenshot {
	return a.screenshots
}

// WriteEvidence writes one evidence file, such as a failure bundle
// entry, into the run folder's artifacts directory under name.
func (a *liveArtifacts) WriteEvidence(name string, data []byte) error {
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.dir, name), data, 0o644)
}
