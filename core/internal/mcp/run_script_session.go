package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/stepstream"
)

const (
	scriptRunSource  = "run_script"
	artifactFileMode = 0o644
)

// scriptTarget is where one script execution reports. Ctx ends when the
// caller gives up or when the run is finalized, and the execution must
// run under it. RunID correlates its log lines with the run folder, and
// Artifacts writes each screenshot into that folder's artifacts
// directory as it is taken. Evidence writes failure bundle files into
// the same directory under names that no other script execution of the
// run reuses.
type scriptTarget struct {
	Ctx       context.Context
	RunID     string
	Artifacts runner.ScriptArtifacts
	Evidence  runner.EvidenceWriter
	// OnStep receives the step events of the execution and appends them to
	// the run folder's step stream.
	OnStep func(runner.StepEvent)
}

// RunScript executes a script inside a run folder. When a run is open,
// the script writes into it and no second run is created, and the run
// stays open afterwards. Ending that run cancels the script and waits
// for it to return, and the step counts of the script are written into
// the run meta when it ends. Otherwise the script is its own run of kind
// mcp, labeled with name, finalized when exec returns:
//
//   - the script passed: passed
//   - the script failed a step: failed
//   - the script aborted because the device was lost: deviceLost
//   - ctx was canceled because the device was lost: deviceLost
//   - ctx was canceled before the script passed: cancelled
//   - the server stopped before the script passed: cancelled
//   - exec returned an error: error
//
// exec builds the runner from the target and runs it under the target's
// context.
func (s *runSession) RunScript(
	ctx context.Context, name string, dev *device.Descriptor,
	exec func(scriptTarget) (runner.ScriptResult, error),
) (runner.ScriptResult, error) {
	s.mu.Lock()
	if open := s.writer; open != nil {
		counter := s.artifacts
		runCtx, end := s.flight.begin(ctx)
		s.mu.Unlock()
		defer end()
		return s.runInOpenRun(runCtx, open, counter, exec)
	}
	runCtx, end := s.bare.begin(ctx)
	s.mu.Unlock()
	defer end()
	return s.runAsOwnRun(runCtx, name, dev, exec)
}

func (s *runSession) runInOpenRun(
	ctx context.Context, open *runfolder.Writer, counter *artifactCounter,
	exec func(scriptTarget) (runner.ScriptResult, error),
) (runner.ScriptResult, error) {
	dir, err := open.ArtifactsDir()
	if err != nil {
		return runner.ScriptResult{}, err
	}
	artifacts := newScriptArtifacts(dir, counter, s.logger)
	result, execErr := exec(scriptTarget{Ctx: ctx, RunID: open.ID(), Artifacts: artifacts, Evidence: artifacts, OnStep: stepstream.NewSink(open, s.logger, stepstream.WithPublisher(s.publishStep))})
	if execErr == nil {
		s.recordScriptTotals(open, result)
	}
	return result, execErr
}

func (s *runSession) runAsOwnRun(
	ctx context.Context, name string, dev *device.Descriptor,
	exec func(scriptTarget) (runner.ScriptResult, error),
) (runner.ScriptResult, error) {
	meta := mcpRunMeta(name, nil, dev)
	meta.ScriptName = name
	meta.MCP.Source = scriptRunSource
	w, err := s.starter.Start(name, meta)
	if err != nil {
		return runner.ScriptResult{}, err
	}
	defer func() { _ = w.Close() }()
	dir, err := w.ArtifactsDir()
	if err != nil {
		return runner.ScriptResult{}, err
	}
	artifacts := newScriptArtifacts(dir, &artifactCounter{}, s.logger)
	s.route(w, false)
	result, execErr := exec(scriptTarget{Ctx: ctx, RunID: w.ID(), Artifacts: artifacts, Evidence: artifacts, OnStep: stepstream.NewSink(w, s.logger, stepstream.WithPublisher(s.publishStep))})
	s.unroute(w)
	finalizeErr := w.Finalize(scriptOutcome(ctx, result, execErr), func(m *runfolder.Meta) {
		m.TotalSteps = result.TotalSteps
		m.FailedAtStep = result.FailedAtStep
		info := *m.MCP
		info.DurationMs = result.DurationMs
		m.MCP = &info
	})
	if finalizeErr != nil {
		s.logger.Warn("run.finalize_failed", logging.LogContext{"id": w.ID(), "error": finalizeErr.Error()})
	}
	return result, execErr
}

func scriptOutcome(ctx context.Context, result runner.ScriptResult, execErr error) runfolder.Outcome {
	switch {
	case execErr == nil && result.Ok:
		return runfolder.OutcomePassed
	case errors.Is(context.Cause(ctx), session.ErrDeviceLost):
		return runfolder.OutcomeDeviceLost
	case ctx.Err() != nil:
		return runfolder.OutcomeCancelled
	case result.Abort != nil && result.Abort.Reason == runner.RunAbortDeviceLost:
		return runfolder.OutcomeDeviceLost
	case execErr != nil:
		return runfolder.OutcomeError
	}
	return runfolder.OutcomeFailed
}

// artifactCounter numbers the artifacts of one run, so screenshots from
// several script executions inside the same run never share a name.
type artifactCounter struct {
	mu sync.Mutex
	n  int
}

func (c *artifactCounter) next() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

// scriptArtifacts is a runner.ScriptArtifacts that writes each
// screenshot into a run folder's artifacts directory the moment it is
// added, so a run that is killed keeps every screenshot taken before it
// stopped. A failed write is logged and the screenshot stays in memory.
//
// Evidence files take a script-NNN- prefix drawn from the run's shared
// counter on the first write, so the failure bundles of several script
// executions in one run never share a name.
type scriptArtifacts struct {
	dir     string
	counter *artifactCounter
	logger  logging.Logger

	mu             sync.Mutex
	screenshots    []runner.Screenshot
	evidencePrefix string
}

func newScriptArtifacts(dir string, counter *artifactCounter, logger logging.Logger) *scriptArtifacts {
	return &scriptArtifacts{dir: dir, counter: counter, logger: logger}
}

func (a *scriptArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	a.mu.Lock()
	a.screenshots = append(a.screenshots, runner.Screenshot{Label: label, Data: data, MimeType: mimeType})
	a.mu.Unlock()

	name := fmt.Sprintf("script-%03d", a.counter.next())
	if label != "" {
		name += "-" + runfolder.Label(label)
	}
	path := filepath.Join(a.dir, name+"."+detectScreenshotExt(data))
	if err := os.WriteFile(path, data, artifactFileMode); err != nil {
		a.logger.Warn("run.artifact_write_failed", logging.LogContext{"path": path, "error": err.Error()})
	}
}

func (a *scriptArtifacts) Screenshots() []runner.Screenshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]runner.Screenshot(nil), a.screenshots...)
}

// WriteEvidence writes one evidence file into the artifacts directory
// under the script's own prefix.
func (a *scriptArtifacts) WriteEvidence(name string, data []byte) error {
	a.mu.Lock()
	if a.evidencePrefix == "" {
		a.evidencePrefix = fmt.Sprintf("script-%03d-", a.counter.next())
	}
	prefix := a.evidencePrefix
	a.mu.Unlock()
	return os.WriteFile(filepath.Join(a.dir, prefix+name), data, artifactFileMode)
}
