package scenario

import (
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
)

// NoPosition is the ChildTarget index for a position the target does
// not have: EntryIndex outside a suite, ScriptIndex for a suite entry
// that is itself a script.
const NoPosition = -1

// ChildTarget identifies one script run inside a scenario or a suite,
// so a host can key the run's artifacts to its place in the whole run.
type ChildTarget struct {
	// EntryIndex is the owning suite entry's position in the suite's
	// entry list, or NoPosition for a standalone scenario.
	EntryIndex int
	// EntryPath is the owning suite entry's path, empty when EntryIndex
	// is NoPosition.
	EntryPath string
	// Attempt is the 1-based attempt number of the owning suite entry.
	// The first attempt and a target outside a suite name their
	// artifacts alike, and a later attempt names its own so it never
	// overwrites an earlier one.
	Attempt int
	// ScriptIndex is the child's position in its scenario's scripts
	// list, or NoPosition when the target is a suite entry that is a
	// script.
	ScriptIndex int
	// ScriptPath is the child's path as the scenario names it, empty
	// when ScriptIndex is NoPosition.
	ScriptPath string
}

// ChildArtifacts is what a host supplies for one child script run: the
// collector the child's screenshots go to, which also persists its
// failure evidence files.
type ChildArtifacts interface {
	runner.ScriptArtifacts
	runner.EvidenceWriter
}

// ArtifactsFactory creates the artifacts of one child script run. The
// runner calls it once per child, in execution order, before the child
// starts, so a host that writes each screenshot when it is taken can
// number files across the whole run. Nil leaves every child with the
// runner's default in-memory collector and no failure capture.
type ArtifactsFactory func(ChildTarget) ChildArtifacts

// forAttempt returns a factory that stamps every target with the suite
// entry attempt it belongs to before delegating, or nil when f is nil.
func (f ArtifactsFactory) forAttempt(index int, path string, attempt int) ArtifactsFactory {
	if f == nil {
		return nil
	}
	return func(target ChildTarget) ChildArtifacts {
		target.EntryIndex, target.EntryPath, target.Attempt = index, path, attempt
		return f(target)
	}
}

// childEvidence builds the artifacts and the failure capture for one
// child. Both are nil when f is nil.
func (f ArtifactsFactory) childEvidence(
	target ChildTarget, o *orchestra.Orchestra, clock orchestra.Clock, logger logging.Logger,
) (runner.ScriptArtifacts, runner.FailureCapture) {
	if f == nil {
		return nil, nil
	}
	artifacts := f(target)
	return artifacts, runner.NewFailureBundle(runner.FailureBundleDeps{
		Orchestra: o, Clock: clock, Logger: logger, Sink: artifacts,
	})
}
