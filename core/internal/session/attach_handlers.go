package session

import (
	"context"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
)

// attachHandlers answers the attach protocol from one session. It reads
// what the session's hub holds, so a viewer never reaches the driver,
// except through a fresh capture that the holder state admits.
type attachHandlers struct {
	s *Session
}

var (
	_ attach.SessionHandler = (*attachHandlers)(nil)
	_ attach.ObserveHandler = (*attachHandlers)(nil)
	_ attach.DebugHandler   = (*attachHandlers)(nil)
)

func (s *Session) attachHandlers() attach.Handlers {
	h := &attachHandlers{s: s}
	handlers := attach.Handlers{Session: h, Observe: h, Viewers: h}
	if s.yielder != nil {
		handlers.Control = s.yielder
	}
	if s.debugger != nil {
		handlers.Debug = h
	}
	return handlers
}

// Status implements attach.SessionHandler.
func (h *attachHandlers) Status() attach.Status {
	info := h.s.Info()
	holder := attach.HolderInfo{
		PID:     info.Holder.PID,
		Command: info.Holder.Command,
		Role:    info.Holder.Role,
		RunID:   info.Holder.RunID,
	}
	if !info.Holder.StartedAt.IsZero() {
		holder.StartedAt = info.Holder.StartedAt.UnixMilli()
	}
	return attach.Status{
		State: string(info.State),
		Device: attach.DeviceInfo{
			ID:       info.Device.ID,
			Platform: string(info.Device.Platform),
			Name:     info.Device.Name,
		},
		Holder:     holder,
		Generation: info.Generation,
	}
}

// TreeLatest implements attach.ObserveHandler. Hub.Latest already masks
// secure nodes, and this serializes nothing else from the snapshot.
func (h *attachHandlers) TreeLatest() attach.TreeResult {
	return treeResult(h.s.Latest())
}

// ScreenshotLatest implements attach.ObserveHandler.
func (h *attachHandlers) ScreenshotLatest() attach.ScreenshotResult {
	return screenshotResult(h.s.Latest())
}

// TreeFresh implements attach.ObserveHandler. Unless the host injected its
// own refresher, a viewer may ask for a fresh capture only while the
// holder is idle, because a run or a tool call in flight owns the device.
func (h *attachHandlers) TreeFresh(ctx context.Context) (attach.FreshResult, error) {
	var snap Snapshot
	var err error
	if refresher := h.s.socketRefresher; refresher != nil {
		snap, err = refresher.FreshTree(ctx)
	} else {
		snap, err = h.s.freshTree(ctx, h.admitFresh)
	}
	if err != nil {
		return attach.FreshResult{}, err
	}
	return attach.FreshResult{
		Tree:          treeResult(snap),
		Screenshot:    screenshotResult(snap),
		Paired:        snap.Paired,
		Highlightable: snap.Highlightable,
		Scale:         snap.Scale,
	}, nil
}

// admitFresh admits a capture while the holder is idle, or while it is
// paused and the capture holds the debug lock.
func (h *attachHandlers) admitFresh(underDebugLock bool) error {
	state := h.s.holderState()
	if state == StateIdle || (state == StatePaused && underDebugLock) {
		return nil
	}
	return diag.NewHolderBusy("the holder is " + string(state))
}

func captureState(stable bool, snap Snapshot) attach.CaptureState {
	return attach.CaptureState{Stable: stable, Paired: snap.Paired, Highlightable: snap.Highlightable, Scale: snap.Scale}
}

func treeResult(snap Snapshot) attach.TreeResult {
	result := attach.TreeResult{Revision: snap.TreeRevision, Epoch: snap.TreeEpoch, CaptureState: captureState(snap.TreeStable, snap)}
	if snap.HasTree {
		tree := snap.Tree
		result.Tree = &tree
	}
	return result
}

func screenshotResult(snap Snapshot) attach.ScreenshotResult {
	result := attach.ScreenshotResult{Revision: snap.ScreenshotRevision, Epoch: snap.ScreenshotEpoch, CaptureState: captureState(snap.ScreenshotStable, snap)}
	if snap.HasScreenshot {
		result.Format = string(snap.ScreenshotFormat)
		result.Image = snap.Screenshot
	}
	return result
}

// Subscribe implements attach.ObserveHandler. The returned channel closes
// when the subscription ends.
func (h *attachHandlers) Subscribe(ctx context.Context, runID string, sinceSeq int64) (<-chan attach.Notice, error) {
	observations, err := h.s.SubscribeSince(ctx, runID, sinceSeq)
	if err != nil {
		return nil, err
	}
	notices := make(chan attach.Notice)
	go h.relay(ctx, observations, notices, stepCursor{runID: runID, seq: sinceSeq})
	return notices, nil
}

// stepCursor is the last step line a subscription delivered.
type stepCursor struct {
	runID string
	seq   int64
}

// relay turns hub observations into notices until the subscription ends.
// Capture observations carry no payload, so each becomes a notice with
// its revision, epoch and capture state. A state observation becomes a
// state notice with the holder state it carries. A step observation is read from the hub
// ring, so a burst of lines costs one read.
func (h *attachHandlers) relay(ctx context.Context, observations <-chan Observation, notices chan<- attach.Notice, cursor stepCursor) {
	defer close(notices)
	send := func(n attach.Notice) bool {
		select {
		case notices <- n:
			return true
		case <-ctx.Done():
			return false
		}
	}
	gapAnnounced := false
	for o := range observations {
		switch o.Kind {
		case ObservedTree:
			if !send(attach.Notice{Type: attach.NoticeTree, Data: attach.TreeNotice{Revision: o.Revision, Epoch: o.Epoch, CaptureState: h.noticeState(o)}}) {
				return
			}
		case ObservedScreenshot:
			if !send(attach.Notice{Type: attach.NoticeScreenshot, Data: attach.ScreenshotNotice{Revision: o.Revision, Epoch: o.Epoch, CaptureState: h.noticeState(o)}}) {
				return
			}
		case ObservedState:
			if !send(attach.Notice{Type: attach.NoticeState, Data: stateNotice(o)}) {
				return
			}
		case ObservedStepGap:
			gapAnnounced = true
			if !send(attach.Notice{Type: attach.NoticeStepGap, Data: attach.StepGapNotice{OldestSeq: int64(o.Revision)}}) {
				return
			}
		case ObservedStep:
			var ok bool
			cursor, ok = h.relaySteps(cursor, gapAnnounced, send)
			gapAnnounced = false
			if !ok {
				return
			}
		case ObservedDropped, ObservedClosed:
			send(attach.Notice{Type: attach.NoticeClosed})
			return
		}
	}
}

func stateNotice(o Observation) attach.StateNotice {
	n := attach.StateNotice{State: string(o.State), Command: o.Command, RunID: o.RunID}
	if o.Pause != nil && o.State == StatePaused {
		n.Pause = &attach.PauseLocation{Source: o.Pause.Source, Line: o.Pause.Line, Depth: o.Pause.Depth, Command: o.Pause.Command}
	}
	return n
}

// noticeState reads the pairing the hub holds when the notice is sent,
// so a notice never reports a pair older than its own capture.
func (h *attachHandlers) noticeState(o Observation) attach.CaptureState {
	paired, highlightable, scale := h.s.hub.pairing()
	return attach.CaptureState{Stable: o.Stable, Paired: paired, Highlightable: highlightable, Scale: scale}
}

// relaySteps sends the held lines after cursor and returns the new
// cursor. It reports false when the subscription should end. A gap the
// subscriber was already told about is not announced twice.
func (h *attachHandlers) relaySteps(cursor stepCursor, gapAnnounced bool, send func(attach.Notice) bool) (stepCursor, bool) {
	lines, gap, err := h.s.StepsSince(cursor.runID, cursor.seq)
	if err != nil {
		return cursor, false
	}
	if gap && !gapAnnounced && len(lines) > 0 {
		if !send(attach.Notice{Type: attach.NoticeStepGap, Data: attach.StepGapNotice{OldestSeq: lines[0].Seq}}) {
			return cursor, false
		}
	}
	epoch := h.s.hub.currentEpoch()
	for _, line := range lines {
		if !send(attach.Notice{Type: attach.NoticeStep, Data: attach.StepNotice{Epoch: epoch, Line: stepLineOf(line)}}) {
			return cursor, false
		}
		cursor = stepCursor{runID: line.RunID, seq: line.Seq}
	}
	return cursor, true
}

func stepLineOf(line runfolder.StepLine) attach.StepLine {
	return attach.StepLine{
		SchemaVersion: line.SchemaVersion,
		RunID:         line.RunID,
		Seq:           line.Seq,
		StepIndex:     line.StepIndex,
		Command:       line.Command,
		Phase:         string(line.Phase),
		Ok:            line.Ok,
		DurationMs:    line.DurationMs,
		Detail:        line.Detail,
		Artifacts:     line.Artifacts,
	}
}
