package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

const forceClosedPredecessorSummary = "force-closed by subsequent start_run, because the agent forgot finish_run"

// RunStarter creates the run folder of a new run. *runfolder.Starter
// satisfies it.
type RunStarter interface {
	Start(label string, base runfolder.Meta) (*runfolder.Writer, error)
}

// runSession owns the server's open run: the in-memory record that bug
// reports attach to, and the run folder it is written into. It is the
// one place that creates, finalizes, and edits MCP runs, so tool
// handlers and the device session only orchestrate over it. It is safe
// for concurrent use.
//
// Finish statuses map to outcomes as follows:
//
//   - passed: passed
//   - failed: failed
//   - error: error
//
// A run the server closes itself takes one of these outcomes:
//
//   - the device was lost: deviceLost
//   - the open-run idle timeout elapsed: cancelled
//   - a later start_run found it still open: error
//
// The idle timeout is cancelled and not interrupted because the server
// closes the run deliberately and records it, while interrupted is what
// a run reads as when its writer vanished without recording anything.
type runSession struct {
	store   *storage.RunStore
	catalog *storage.RunCatalog
	starter RunStarter
	root    string
	clock   orchestra.Clock
	logger  logging.Logger

	mu        sync.Mutex
	writer    *runfolder.Writer
	artifacts *artifactCounter
}

type runSessionDeps struct {
	Store   *storage.RunStore
	Catalog *storage.RunCatalog
	Starter RunStarter
	Root    string
	Clock   orchestra.Clock
	Logger  logging.Logger
}

func newRunSession(deps runSessionDeps) *runSession {
	return &runSession{
		store: deps.Store, catalog: deps.Catalog, starter: deps.Starter,
		root: deps.Root, clock: deps.Clock, logger: deps.Logger, artifacts: &artifactCounter{},
	}
}

// startedRun is the result of runSession.Start. Superseded is the
// previously open run when this start closed it.
type startedRun struct {
	Run        *storage.RunRecord
	Superseded *storage.RunRecord
}

// finishedRun is the result of runSession.Finish.
type finishedRun struct {
	Run        *storage.RunRecord
	DurationMs int64
}

// OpenRunID reports the id of the open run, or an empty string when no
// run is open.
func (s *runSession) OpenRunID() string {
	if run := s.store.Current(); run != nil {
		return run.ID
	}
	return ""
}

// Start opens a new run in its own run folder. The folder id is labeled
// with the selected device's name, or with a neutral label when no
// device is selected. A run still open is finalized as an error first.
// When the folder cannot be created, the open run is left as it was.
func (s *runSession) Start(name string, source *string, dev *device.Descriptor) (startedRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writer, err := s.starter.Start(deviceLabel(dev), mcpRunMeta(name, source, dev))
	if err != nil {
		return startedRun{}, err
	}
	started := s.store.Start(storage.StartOptions{ID: writer.ID(), Name: name, Source: source})
	if predecessor := started.ErroredPredecessor; predecessor != nil {
		summary := forceClosedPredecessorSummary
		s.finalizeLocked(s.writer, predecessor, runfolder.OutcomeError, &summary)
		s.logger.Warn("run.force_closed_predecessor", logging.LogContext{
			"id": predecessor.ID, "newRunId": started.Run.ID,
		})
	}
	s.writer, s.artifacts = writer, &artifactCounter{}
	return startedRun{Run: started.Run, Superseded: started.ErroredPredecessor}, nil
}

// Finish closes the open run with the outcome of status, which is
// passed, failed, or error and defaults to passed when empty. It returns
// a nil run when none is open.
func (s *runSession) Finish(status string, summary *string) finishedRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome := outcomeOfFinishStatus(status)
	run := s.store.Finish(legacyStatusOf(outcome))
	if run == nil {
		return finishedRun{}
	}
	s.finalizeLocked(s.writer, run, outcome, summary)
	s.writer = nil
	return finishedRun{Run: run, DurationMs: durationOf(run)}
}

// CloseOpenRun finalizes the open run with outcome and summary on the
// agent's behalf, and returns the closed run's id. It returns an empty
// id when no run was open.
func (s *runSession) CloseOpenRun(outcome runfolder.Outcome, summary string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.store.Finish(legacyStatusOf(outcome))
	if run == nil {
		return ""
	}
	s.finalizeLocked(s.writer, run, outcome, &summary)
	s.writer = nil
	s.logger.Info("run.closed_by_session", logging.LogContext{"id": run.ID, "outcome": string(outcome)})
	return run.ID
}

// SaveScreenshot writes an image into the open run's artifacts
// directory, or into the loose screenshots directory under the storage
// root when no run is open, and returns the file path.
func (s *runSession) SaveScreenshot(data []byte) (string, error) {
	s.mu.Lock()
	open := s.writer
	s.mu.Unlock()

	var dir string
	if open != nil {
		var err error
		if dir, err = open.ArtifactsDir(); err != nil {
			return "", err
		}
	} else {
		dir = runfolder.LooseScreenshotsDir(s.root)
		if err := os.MkdirAll(dir, screenshotDirMode); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("screenshot-%d.%s", s.clock.NowMs(), detectScreenshotExt(data)))
	if err := os.WriteFile(path, data, artifactFileMode); err != nil {
		return "", err
	}
	return path, nil
}

// SetSummary replaces the summary of the run with the given id and
// reports whether the run exists. The open run is edited through its
// live writer, and any other run through the catalog.
func (s *runSession) SetSummary(id, summary string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil && s.writer.ID() == id {
		err := s.writer.Update(func(m *runfolder.Meta) { m.MCP = withSummary(m.MCP, summary) })
		return err == nil, err
	}
	return s.catalog.SetSummary(id, summary)
}

// finalizeLocked records the end of run in its folder. A folder that
// cannot be finalized is closed so its lock is released, and the
// failure is logged because the in-memory run is already closed.
func (s *runSession) finalizeLocked(w *runfolder.Writer, run *storage.RunRecord, outcome runfolder.Outcome, summary *string) {
	if w == nil {
		return
	}
	err := w.Finalize(outcome, func(m *runfolder.Meta) {
		info := mcpInfoOf(run)
		info.Summary = summary
		if summary == nil && m.MCP != nil {
			info.Summary = m.MCP.Summary
		}
		m.MCP = &info
	})
	if err != nil {
		s.logger.Warn("run.finalize_failed", logging.LogContext{"id": run.ID, "error": err.Error()})
		_ = w.Close()
	}
}

func withSummary(info *runfolder.MCPInfo, summary string) *runfolder.MCPInfo {
	var next runfolder.MCPInfo
	if info != nil {
		next = *info
	}
	next.Summary = &summary
	return &next
}

func mcpRunMeta(name string, source *string, dev *device.Descriptor) runfolder.Meta {
	info := &runfolder.MCPInfo{Name: name, Source: "unknown"}
	if source != nil {
		info.Source = *source
	}
	meta := runfolder.Meta{Kind: runfolder.KindMCP, MCP: info}
	if dev != nil {
		meta.DeviceID = dev.ID
		meta.Device = &runfolder.Device{
			Platform: string(dev.Platform), Kind: string(dev.Kind), HardwareID: dev.ID, Name: dev.Name,
		}
	}
	return meta
}

func mcpInfoOf(run *storage.RunRecord) runfolder.MCPInfo {
	info := runfolder.MCPInfo{
		Name: run.Name, Source: run.Source, ActionCount: run.ActionCount, DurationMs: durationOf(run),
	}
	for _, f := range run.Findings {
		info.Findings = append(info.Findings, runfolder.Finding{
			ID: f.ID, Title: f.Title, Description: f.Description,
			ScreenshotPath: f.ScreenshotPath, Timestamp: f.Timestamp,
		})
	}
	return info
}

func durationOf(run *storage.RunRecord) int64 {
	if run.FinishedAt == nil {
		return 0
	}
	return *run.FinishedAt - run.StartedAt
}

// deviceLabel names a run folder after the selected device, falling
// back to its id, and to the neutral default when none is selected.
func deviceLabel(dev *device.Descriptor) string {
	switch {
	case dev == nil:
		return runfolder.DefaultLabel
	case dev.Name != "":
		return dev.Name
	}
	return dev.ID
}

// outcomeOfFinishStatus maps a finish_run status to the outcome the run
// folder records. An unknown or empty status counts as passed.
func outcomeOfFinishStatus(status string) runfolder.Outcome {
	switch status {
	case string(storage.RunStatusFailed):
		return runfolder.OutcomeFailed
	case string(storage.RunStatusError):
		return runfolder.OutcomeError
	}
	return runfolder.OutcomePassed
}

// legacyStatusOf maps an outcome to the status the in-memory run
// carries and the tool payloads keep reporting.
func legacyStatusOf(outcome runfolder.Outcome) storage.RunStatus {
	return storage.RunStatus(runfolder.LegacyStatusOf(runfolder.Meta{Outcome: outcome}))
}
