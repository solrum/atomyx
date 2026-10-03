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
)

func (s *Session) attachHandlers() attach.Handlers {
	h := &attachHandlers{s: s}
	return attach.Handlers{Session: h, Observe: h, Viewers: h}
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

// TreeFresh implements attach.ObserveHandler. A viewer may ask for a fresh
// capture only while the holder is idle, because a run or a tool call in
// flight owns the device.
func (h *attachHandlers) TreeFresh(ctx context.Context) (attach.FreshResult, error) {
	snap, err := h.s.FreshTreeIf(ctx, h.admitFresh)
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

func (h *attachHandlers) admitFresh() error {
	if state := h.s.holderState(); state != StateIdle {
		return diag.NewHolderBusy("the holder is " + string(state))
	}
	return nil
}

func treeResult(snap Snapshot) attach.TreeResult {
	result := attach.TreeResult{Revision: snap.TreeRevision, Epoch: snap.TreeEpoch}
	if snap.HasTree {
		tree := snap.Tree
		result.Tree = &tree
	}
	return result
}

func screenshotResult(snap Snapshot) attach.ScreenshotResult {
	result := attach.ScreenshotResult{Revision: snap.ScreenshotRevision, Epoch: snap.ScreenshotEpoch}
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
// its revision and epoch. A step observation is read from the hub
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
			if !send(attach.Notice{Type: attach.NoticeTree, Data: attach.TreeNotice{Revision: o.Revision, Epoch: o.Epoch}}) {
				return
			}
		case ObservedScreenshot:
			if !send(attach.Notice{Type: attach.NoticeScreenshot, Data: attach.ScreenshotNotice{Revision: o.Revision, Epoch: o.Epoch}}) {
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
