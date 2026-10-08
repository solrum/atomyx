package cli

import (
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/runfolder"
)

func TestScreenshotFileIsReportedRunRelativeAsItIsWritten(t *testing.T) {
	runDir := t.TempDir()
	var got []string
	seq := newArtifactSequence(runfolder.ArtifactsDir(runDir))
	seq.onFile = func(file string) { got = append(got, file) }

	seq.forScope("root").AddScreenshot("home", []byte("png"), "image/png")

	if len(got) != 1 || filepath.Dir(got[0]) != "artifacts" {
		t.Fatalf("reported files = %v, want one path under artifacts/", got)
	}
	if _, err := os.Stat(filepath.Join(runDir, filepath.FromSlash(got[0]))); err != nil {
		t.Fatalf("the reported file does not exist in the run folder: %v", err)
	}
}
