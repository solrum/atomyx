package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// artifactSequence numbers every screenshot of one run in the order the
// run takes them, and writes each into the run folder's artifacts
// directory the moment it is added, so a run that crashes or is killed
// keeps every screenshot taken before it stopped.
//
// File names come from report.BuildScreenshotIndex over the whole run's
// screenshots in order, so they match the names the report writer
// assigns when it rewrites the same screenshots at the end of the run
// and the report links stay valid. A failed early write is not fatal
// because the report writer repeats every write and reports its own
// failure.
type artifactSequence struct {
	dir string
	// onFile, when set, receives the run-relative path of each screenshot
	// file as it is written.
	onFile func(string)

	mu    sync.Mutex
	shots []report.ReportScreenshot
}

func newArtifactSequence(dir string) *artifactSequence {
	return &artifactSequence{dir: dir}
}

// fileFor records one screenshot and returns the file name the report
// writer assigns it.
func (q *artifactSequence) fileFor(scope, label, mimeType string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shots = append(q.shots, report.ReportScreenshot{Scope: scope, Label: label, MimeType: mimeType})
	refs := report.BuildScreenshotIndex(q.shots).Refs
	return refs[len(refs)-1].File
}

func (q *artifactSequence) write(name string, data []byte) error {
	if err := os.MkdirAll(q.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(q.dir, name), data, 0o644)
}

// forScope returns the artifacts of one script whose screenshots carry
// scope. Failure evidence files take a prefix derived from scope, so
// the evidence of different scripts never shares a name. The root scope
// adds no prefix.
func (q *artifactSequence) forScope(scope string) *liveArtifacts {
	return &liveArtifacts{seq: q, scope: scope, evidencePrefix: evidencePrefix(scope)}
}

// forTarget returns the artifacts of one child script of a scenario or
// suite, scoped exactly as the report scopes that child's screenshots.
func (q *artifactSequence) forTarget(target scenario.ChildTarget) scenario.ChildArtifacts {
	return q.forScope(scopeForTarget(target))
}

func scopeForTarget(target scenario.ChildTarget) string {
	switch {
	case target.EntryIndex == scenario.NoPosition:
		return report.ScenarioChildScope(report.RootScriptScope, target.ScriptIndex, target.ScriptPath)
	case target.ScriptIndex == scenario.NoPosition:
		return report.SuiteEntryScope(target.EntryIndex, target.EntryPath)
	default:
		entry := report.SuiteEntryScope(target.EntryIndex, target.EntryPath)
		return report.ScenarioChildScope(entry, target.ScriptIndex, target.ScriptPath)
	}
}

var nonAlphanumericRun = regexp.MustCompile(`[^a-z0-9]+`)

const maxEvidencePrefixLen = 80

func evidencePrefix(scope string) string {
	slug := strings.Trim(nonAlphanumericRun.ReplaceAllString(strings.ToLower(scope), "-"), "-")
	if len(slug) > maxEvidencePrefixLen {
		slug = slug[:maxEvidencePrefixLen]
	}
	if slug == "" {
		return ""
	}
	return slug + "-"
}

// liveArtifacts is a runner.ScriptArtifacts for one script that writes
// each screenshot into the run folder's artifacts directory the moment
// it is added. It is also the runner.EvidenceWriter for that script's
// failure bundle.
type liveArtifacts struct {
	seq            *artifactSequence
	scope          string
	evidencePrefix string
	screenshots    []runner.Screenshot
}

func newLiveArtifacts(dir string) *liveArtifacts {
	return newArtifactSequence(dir).forScope(report.RootScriptScope)
}

func (a *liveArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	a.screenshots = append(a.screenshots, runner.Screenshot{Label: label, Data: data, MimeType: mimeType})
	name := a.seq.fileFor(a.scope, label, mimeType)
	_ = a.seq.write(name, data)
	if a.seq.onFile != nil {
		a.seq.onFile(filepath.ToSlash(filepath.Join(filepath.Base(a.seq.dir), name)))
	}
}

func (a *liveArtifacts) Screenshots() []runner.Screenshot {
	return a.screenshots
}

// WriteEvidence writes one evidence file, such as a failure bundle
// entry, into the artifacts directory under the script's own prefix.
func (a *liveArtifacts) WriteEvidence(name string, data []byte) error {
	return a.seq.write(a.evidencePrefix+name, data)
}
