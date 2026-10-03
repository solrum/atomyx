package session

import (
	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
)

// Kinds of Intervention.
const (
	InterventionViewerAttached = "viewerAttached"
	InterventionViewerDetached = "viewerDetached"
)

// maxPendingInterventions bounds the entries a session keeps for its
// drainer. A host that never drains, such as a CLI command, keeps only
// the newest entries.
const maxPendingInterventions = 256

// maxUnrecordedInterventions bounds the entries kept for the recorder a
// host has not yet set. A host that never sets one keeps only the newest.
const maxUnrecordedInterventions = 64

// Intervention is something another process did to the holder, such as
// attaching a viewer. AtMs is the time in epoch milliseconds and Client
// is the name the process gave.
type Intervention struct {
	Kind   string `json:"kind"`
	AtMs   int64  `json:"atMs"`
	Client string `json:"client"`
}

// InterventionSource hands the interventions that happened since the
// last call to its owner.
type InterventionSource interface {
	// Interventions returns the entries recorded since the previous call,
	// oldest first, and clears them.
	Interventions() []Intervention
}

// InterventionRecorder receives each intervention while a run is open, so
// the run folder keeps what happened during the run.
// *runfolder.Writer satisfies it.
type InterventionRecorder interface {
	AppendIntervention(runfolder.InterventionLine) error
}

var (
	_ InterventionSource   = (*Session)(nil)
	_ attach.ViewerHandler = (*attachHandlers)(nil)
)

// Interventions implements InterventionSource.
func (s *Session) Interventions() []Intervention {
	s.interventionMu.Lock()
	defer s.interventionMu.Unlock()
	drained := s.interventions
	s.interventions = nil
	return drained
}

// SetInterventionRecorder sets where interventions are appended while a
// run is open. A host sets it when a run starts and passes nil when the
// run ends, before it closes the run folder.
//
// The first call also writes to a non-nil recorder the entries recorded
// before it, up to maxUnrecordedInterventions of the newest, because a
// viewer can attach between the session opening and the host deciding.
// Calling it with nil returns only after any append already in flight has
// finished, so the host may close the run folder right after.
func (s *Session) SetInterventionRecorder(recorder InterventionRecorder) {
	s.interventionMu.Lock()
	defer s.interventionMu.Unlock()
	s.recorder = recorder
	s.recorderDecided = true
	early := s.unrecorded
	s.unrecorded = nil
	if recorder == nil {
		return
	}
	for _, entry := range early {
		s.appendIntervention(entry)
	}
}

func (s *Session) recordIntervention(kind, client string) {
	entry := Intervention{Kind: kind, AtMs: s.deps.Clock.NowMs(), Client: client}
	s.interventionMu.Lock()
	defer s.interventionMu.Unlock()
	if len(s.interventions) >= maxPendingInterventions {
		s.interventions = s.interventions[1:]
	}
	s.interventions = append(s.interventions, entry)
	switch {
	case s.recorder != nil:
		s.appendIntervention(entry)
	case !s.recorderDecided:
		if len(s.unrecorded) >= maxUnrecordedInterventions {
			s.unrecorded = s.unrecorded[1:]
		}
		s.unrecorded = append(s.unrecorded, entry)
	}
}

// appendIntervention writes entry to the recorder. The caller holds
// interventionMu and has checked that a recorder is set. The lock stays
// held across the disk write for two reasons. Entries reach the run
// folder in the order they were recorded. A host that clears the recorder
// is also guaranteed that no write is in flight afterwards.
func (s *Session) appendIntervention(entry Intervention) {
	line := runfolder.InterventionLine{Kind: entry.Kind, AtMs: entry.AtMs, Client: entry.Client}
	if err := s.recorder.AppendIntervention(line); err != nil {
		s.deps.Logger.Warn("run.intervention_line_failed", logging.LogContext{"error": err.Error()})
	}
}

// ViewerAttached implements attach.ViewerHandler.
func (h *attachHandlers) ViewerAttached(info attach.ViewerInfo) {
	h.s.recordIntervention(InterventionViewerAttached, info.Client)
}

// ViewerDetached implements attach.ViewerHandler.
func (h *attachHandlers) ViewerDetached(info attach.ViewerInfo) {
	h.s.recordIntervention(InterventionViewerDetached, info.Client)
}
