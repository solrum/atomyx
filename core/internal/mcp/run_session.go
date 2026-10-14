package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/recording"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
)

const (
	forceClosedPredecessorSummary = "force-closed by subsequent start_run, because the agent forgot finish_run"
	shutdownSummary               = "closed because the server stopped"

	// runDrainTimeout bounds how long a close the server starts itself
	// waits for the scripts of a run to return before it finalizes the
	// run anyway.
	runDrainTimeout = 5 * time.Second
)

// RunStarter creates the run folder of a new run and names the storage
// root those folders live under. The root it returns is the single
// source for everything the server reads or writes beneath the storage
// root, so no second root value can disagree with it. *runfolder.Starter
// satisfies it.
type RunStarter interface {
	Root() string
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
//   - the server stopped: cancelled
//   - a later start_run found it still open: error
//
// The idle timeout is cancelled and not interrupted because the server
// closes the run deliberately and records it, while interrupted is what
// a run reads as when its writer vanished without recording anything.
//
// Scripts run inside the open run are tracked while they execute. Every
// way of ending the open run cancels them and waits for them to return
// before the run is finalized, so a script never writes into a
// finalized run. A finish_run waits only as long as its caller allows
// and leaves the run open when that runs out. A close the server starts
// itself waits at most drainTimeout and then finalizes anyway. Scripts
// that are their own run are tracked apart and are ended only when the
// server stops.
//
// The TotalSteps and FailedAtStep of the run meta describe the script
// that ended last, so a later script replaces the counts of an earlier
// one.
type runSession struct {
	store                   *storage.RunStore
	catalog                 *storage.RunCatalog
	starter                 RunStarter
	clock                   orchestra.Clock
	logger                  logging.Logger
	drainTimeout            time.Duration
	bare                    *scriptFlight
	publishStep             func(runfolder.StepLine)
	setInterventionRecorder func(session.InterventionRecorder)
	// recordings records the screen of a script that is its own run. Nil
	// means recording is off.
	recordings *recording.Lifecycle
	// agentInfo reports the agent versions of the bound device, nil when
	// the host tracks none.
	agentInfo func() driver.AgentInfo

	routeMu sync.Mutex
	routed  *runfolder.Writer

	mu        sync.Mutex
	writer    *runfolder.Writer
	artifacts *artifactCounter
	flight    *scriptFlight
}

type runSessionDeps struct {
	Store   *storage.RunStore
	Catalog *storage.RunCatalog
	Starter RunStarter
	Clock   orchestra.Clock
	Logger  logging.Logger
	// PublishStep receives every step line a script run appends, so the
	// viewers of the held device follow the run. Nil publishes nothing.
	PublishStep func(runfolder.StepLine)
	// SetInterventionRecorder receives the writer of the run whose folder
	// should keep interventions, and nil when that run ends. Nil routes
	// nothing.
	SetInterventionRecorder func(session.InterventionRecorder)
	// Recordings, when non-nil, records the screen of every script that is
	// its own run and keeps the video of the ones that fail. Nil records
	// nothing.
	Recordings *recording.Lifecycle
	// AgentInfo reports the agent versions of the bound device, zero when
	// none is bound. Nil records no agent versions.
	AgentInfo func() driver.AgentInfo
}

func newRunSession(deps runSessionDeps) *runSession {
	return &runSession{
		store: deps.Store, catalog: deps.Catalog, starter: deps.Starter,
		clock: deps.Clock, logger: deps.Logger, artifacts: &artifactCounter{},
		drainTimeout: runDrainTimeout, bare: newScriptFlight(), flight: newScriptFlight(), publishStep: deps.PublishStep, setInterventionRecorder: deps.SetInterventionRecorder, recordings: deps.Recordings, agentInfo: deps.AgentInfo,
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
// device is selected. A run still open is finalized as an error first,
// after its scripts return or ctx ends. When the folder cannot be
// created, the open run is left as it was.
func (s *runSession) Start(ctx context.Context, name string, source *string, dev *device.Descriptor) (startedRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	writer, err := s.starter.Start(deviceLabel(dev), s.runMeta(name, source, dev))
	if err != nil {
		return startedRun{}, err
	}
	started := s.store.Start(storage.StartOptions{ID: writer.ID(), Name: name, Source: source})
	if predecessor := started.ErroredPredecessor; predecessor != nil {
		s.drainLocked(ctx)
		_ = s.finalizeLocked(s.writer, predecessor, runfolder.OutcomeError, nil, forceClosedPredecessorSummary)
		s.logger.Warn("run.force_closed_predecessor", logging.LogContext{
			"id": predecessor.ID, "newRunId": started.Run.ID,
		})
	}
	s.writer, s.artifacts, s.flight = writer, &artifactCounter{}, newScriptFlight()
	s.route(writer, true)
	return startedRun{Run: started.Run, Superseded: started.ErroredPredecessor}, nil
}

// Finish closes the open run with the outcome of status, which is
// passed, failed, or error and defaults to passed when empty. It cancels
// the scripts running in the run and waits for them to return first. It
// returns a nil run when none is open. It returns an error and leaves
// the run open when ctx ends before the scripts return. It returns the
// closed run together with an error when the run folder could not be
// finalized, because the run then reads as interrupted.
func (s *runSession) Finish(ctx context.Context, status string, summary *string) (finishedRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Current() == nil {
		return finishedRun{}, nil
	}
	if err := s.flight.cancelAndWait(ctx); err != nil {
		return finishedRun{}, fmt.Errorf("the run still has a script executing, so it was not finished: %w", err)
	}
	outcome := outcomeOfFinishStatus(status)
	run := s.store.Finish(legacyStatusOf(outcome))
	err := s.finalizeLocked(s.writer, run, outcome, summary, "")
	s.writer = nil
	return finishedRun{Run: run, DurationMs: durationOf(run)}, err
}

// CancelScriptsForLostDevice cancels every script the session is
// running, in the open run and outside it, with cause as the
// cancellation cause, so each reports a lost device and not a user
// stop. It does not wait: a script that is its own run finalizes that
// run when it returns.
func (s *runSession) CancelScriptsForLostDevice(cause error) {
	s.mu.Lock()
	open := s.flight
	s.mu.Unlock()
	open.cancelAll(cause)
	s.bare.cancelAll(cause)
}

// CloseOpenRun finalizes the open run with outcome on the server's
// behalf, and returns the closed run's id. It returns an empty id when
// no run was open. fallbackSummary becomes the summary only when the
// agent has not set one. A failure to finalize is logged.
func (s *runSession) CloseOpenRun(outcome runfolder.Outcome, fallbackSummary string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store.Current() == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.drainTimeout)
	defer cancel()
	s.drainLocked(ctx)
	run := s.store.Finish(legacyStatusOf(outcome))
	_ = s.finalizeLocked(s.writer, run, outcome, nil, fallbackSummary)
	s.writer = nil
	s.logger.Info("run.closed_by_session", logging.LogContext{"id": run.ID, "outcome": string(outcome)})
	return run.ID
}

// Close ends everything the session owns when the server stops. The
// open run is finalized as cancelled, and every script that is its own
// run is cancelled and given until drainTimeout to finalize that run.
// Closing again is a no-op.
func (s *runSession) Close() {
	s.CloseOpenRun(runfolder.OutcomeCancelled, shutdownSummary)
	ctx, cancel := context.WithTimeout(context.Background(), s.drainTimeout)
	defer cancel()
	if err := s.bare.cancelAndWait(ctx); err != nil {
		s.logger.Warn("run.script_drain_incomplete", logging.LogContext{"error": err.Error()})
	}
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
		dir = runfolder.LooseScreenshotsDir(s.starter.Root())
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

// drainLocked cancels the scripts running in the open run and waits for
// them until ctx ends. An incomplete wait is logged, because a close
// the server decides on cannot be held back by a script that ignores
// cancellation.
func (s *runSession) drainLocked(ctx context.Context) {
	if err := s.flight.cancelAndWait(ctx); err != nil {
		s.logger.Warn("run.script_drain_incomplete", logging.LogContext{"error": err.Error()})
	}
}

// recordScriptTotals writes the step counts of a script that just ended
// into the meta of the run it ran in.
func (s *runSession) recordScriptTotals(w *runfolder.Writer, result runner.ScriptResult) {
	err := w.Update(func(m *runfolder.Meta) {
		m.TotalSteps = result.TotalSteps
		m.FailedAtStep = result.FailedAtStep
	})
	if err != nil {
		s.logger.Warn("run.script_totals_failed", logging.LogContext{"id": w.ID(), "error": err.Error()})
	}
}

// finalizeLocked records the end of run in its folder. summary replaces
// the stored summary when not nil, and fallbackSummary fills it only
// when none is stored, so a summary the agent wrote is never replaced
// by one the server composed. A folder that cannot be finalized is
// closed so its lock is released, and the failure is logged and
// returned, because the in-memory run is already closed and the folder
// reads as interrupted.
func (s *runSession) finalizeLocked(w *runfolder.Writer, run *storage.RunRecord, outcome runfolder.Outcome, summary *string, fallbackSummary string) error {
	if w == nil {
		return nil
	}
	s.unroute(w)
	err := w.Finalize(outcome, func(m *runfolder.Meta) {
		info := mcpInfoOf(run)
		if m.MCP != nil {
			info.Summary = m.MCP.Summary
		}
		if summary != nil {
			info.Summary = summary
		}
		if fallbackSummary != "" && (info.Summary == nil || *info.Summary == "") {
			info.Summary = &fallbackSummary
		}
		m.MCP = &info
	})
	if err != nil {
		s.logger.Warn("run.finalize_failed", logging.LogContext{"id": run.ID, "error": err.Error()})
		_ = w.Close()
		return fmt.Errorf("run %s was closed but its run folder could not be finalized, so it reads as interrupted: %w", run.ID, err)
	}
	return nil
}

func withSummary(info *runfolder.MCPInfo, summary string) *runfolder.MCPInfo {
	var next runfolder.MCPInfo
	if info != nil {
		next = *info
	}
	next.Summary = &summary
	return &next
}

// runMeta builds the starting meta of an MCP run and records the agent
// versions of the bound device in it.
func (s *runSession) runMeta(name string, source *string, dev *device.Descriptor) runfolder.Meta {
	meta := mcpRunMeta(name, source, dev)
	if s.agentInfo != nil {
		info := s.agentInfo()
		runfolder.RecordAgent(&meta, info.ProtocolVersion, info.AgentVersion)
	}
	return meta
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

// RecordAgent stores the agent versions of a newly bound driver in the
// run the device is serving, unless that run already recorded versions.
// It covers a run that starts while the device is released, since no
// driver is bound to read versions from at that point. It takes only the
// routing lock, because a script cancelled by Finish may bind the device
// while Finish holds the run lock.
func (s *runSession) RecordAgent(info driver.AgentInfo) {
	if info == (driver.AgentInfo{}) {
		return
	}
	s.routeMu.Lock()
	w := s.routed
	s.routeMu.Unlock()
	if w == nil {
		return
	}
	err := w.Update(func(m *runfolder.Meta) {
		if m.Host != nil && (m.Host.ProtocolVersion != 0 || m.Host.AgentVersion != "") {
			return
		}
		runfolder.RecordAgent(m, info.ProtocolVersion, info.AgentVersion)
	})
	if err != nil {
		s.logger.Debug("run.record_agent_failed", logging.LogContext{"id": w.ID(), "error": err.Error()})
	}
}

// route sends the interventions of the held device to w until unroute.
// An unconditional route replaces the current one. A conditional route
// leaves a run that is already routed alone and reports whether it took
// over, so a script that is its own run never steals interventions from
// an open run.
func (s *runSession) route(w *runfolder.Writer, replace bool) bool {
	if s.setInterventionRecorder == nil {
		return false
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	if !replace && s.routed != nil {
		return false
	}
	s.routed = w
	s.setInterventionRecorder(w)
	return true
}

// unroute stops sending interventions to w. It does nothing when another
// run was routed in the meantime.
func (s *runSession) unroute(w *runfolder.Writer) {
	if s.setInterventionRecorder == nil {
		return
	}
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	if s.routed != w {
		return
	}
	s.routed = nil
	s.setInterventionRecorder(nil)
}
