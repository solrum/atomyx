package mcp

import (
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/storage"
)

// runTracker is the DeviceSession's view of the server's open run: it
// reports whether a run is open and closes one the session ended on the
// agent's behalf. It owns no run state of its own.
type runTracker struct {
	runs    *storage.RunStore
	storage storage.Storage
	clock   orchestra.Clock
	logger  logging.Logger
}

// OpenRunID reports the id of the open run, or an empty string when no
// run is open.
func (t *runTracker) OpenRunID() string {
	if run := t.runs.Current(); run != nil {
		return run.ID
	}
	return ""
}

// CloseOpenRun finishes the open run with status and persists its
// record with summary, and returns the closed run's id. It returns an
// empty id when no run was open. A persistence failure is logged and
// does not stop the close, because the in-memory run is already closed.
func (t *runTracker) CloseOpenRun(status storage.RunStatus, summary string) string {
	run := t.runs.Finish(status)
	if run == nil {
		return ""
	}
	if t.storage != nil {
		persisted := storage.NewPersistedRun(run, &summary, t.clock.NowMs())
		if err := t.storage.Save(storage.RunKey(run.ID), persisted); err != nil {
			t.logger.Warn("run.close_persist_failed", logging.LogContext{"id": run.ID, "error": err.Error()})
		}
	}
	t.logger.Info("run.closed_by_session", logging.LogContext{"id": run.ID, "status": string(status)})
	return run.ID
}
