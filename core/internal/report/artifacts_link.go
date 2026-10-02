package report

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ArtifactsLink returns the link prefix that leads from a file inside
// reportDir to a file inside artifactsDir, as a slash-separated
// relative path that ends in a slash. Each path segment is
// percent-encoded, and the dot-dot segments of a parent step are kept
// as they are. Both directories must be absolute or both relative to
// the same base.
func ArtifactsLink(reportDir, artifactsDir string) (string, error) {
	rel, err := filepath.Rel(reportDir, artifactsDir)
	if err != nil {
		return "", fmt.Errorf("report: computing the link from %q to %q: %w", reportDir, artifactsDir, err)
	}
	if rel == "." {
		return "", nil
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for i, segment := range segments {
		if segment != ".." {
			segments[i] = encodeURIComponent(segment)
		}
	}
	return strings.Join(segments, "/") + "/", nil
}
