package session

import (
	"context"

	"dev.atomyx/internal/attach"
)

// SetBreakpoints implements attach.DebugHandler.
func (h *attachHandlers) SetBreakpoints(locations []string) error {
	return h.s.SetBreakpoints(locations)
}

// Pause implements attach.DebugHandler.
func (h *attachHandlers) Pause() error { return h.s.Pause() }

// Resume implements attach.DebugHandler.
func (h *attachHandlers) Resume(ctx context.Context, step, force bool) (attach.DebugResumeResult, error) {
	result, err := h.s.Resume(ctx, ResumeOptions{Step: step, Force: force})
	if err != nil {
		return attach.DebugResumeResult{}, err
	}
	return attach.DebugResumeResult{Resumed: result.Resumed, MissReason: result.MissReason}, nil
}

// Variables implements attach.DebugHandler.
func (h *attachHandlers) Variables() (attach.DebugVariables, error) {
	view, err := h.s.Variables()
	if err != nil {
		return attach.DebugVariables{}, err
	}
	variables := view.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	return attach.DebugVariables{Variables: variables}, nil
}

// Act implements attach.DebugHandler.
func (h *attachHandlers) Act(ctx context.Context, stepYAML string) (attach.ActResult, error) {
	result, err := h.s.Act(ctx, stepYAML)
	if err != nil {
		return attach.ActResult{}, err
	}
	return attach.ActResult{
		Command:    result.Command,
		Outcome:    result.Outcome,
		Message:    result.Message,
		DurationMs: result.DurationMs,
		Artifacts:  result.Artifacts,
	}, nil
}

// DebugViewers reports how many attached viewers have sent a debug or act
// request and are still connected.
func (s *Session) DebugViewers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.debugViewers)
}

// ViewerDebugging implements attach.ViewerHandler.
func (h *attachHandlers) ViewerDebugging(info attach.ViewerInfo) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if h.s.debugViewers == nil {
		h.s.debugViewers = map[uint64]struct{}{}
	}
	h.s.debugViewers[info.ID] = struct{}{}
}

// ViewerStoppedDebugging implements attach.ViewerHandler.
func (h *attachHandlers) ViewerStoppedDebugging(info attach.ViewerInfo) {
	h.s.mu.Lock()
	_, was := h.s.debugViewers[info.ID]
	delete(h.s.debugViewers, info.ID)
	last := was && len(h.s.debugViewers) == 0
	notify := h.s.onLastDebugViewerLeft
	h.s.mu.Unlock()
	if last && notify != nil {
		notify()
	}
}
