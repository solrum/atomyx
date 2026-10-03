package session

import (
	"context"

	"dev.atomyx/internal/attach"
)

// pump follows the holder's notices until the holder is gone. When a
// subscription ends while the connection lives, the pump subscribes again
// on that connection after the last step line it stored. It dials a new
// connection only when the connection itself is gone.
func (h *RemoteHandle) pump(ctx context.Context) {
	defer h.finish()
	client := h.currentClient()
	stale := false
	for {
		restart := h.consume(ctx, client, stale)
		if ctx.Err() != nil {
			return
		}
		if restart && h.resubscribe(ctx, client) {
			stale = true
			continue
		}
		next, ok := h.redial(ctx)
		if !ok {
			return
		}
		client, stale = next, true
	}
}

// consume handles notices until the connection ends, and reports whether
// the subscription must be started again. That is the case when the
// client dropped step notices, and when the holder ended the subscription,
// which it does for a viewer that fell behind as well as for a holder that
// is closing. Starting again fails when the holder is gone. While stale is
// set, notices of the replaced subscription are skipped up to the marker
// that announces the new one.
func (h *RemoteHandle) consume(ctx context.Context, client *attach.Client, stale bool) (restart bool) {
	for f := range client.Notices() {
		if stale {
			stale = f.Type != attach.NoticeSubscribed
			continue
		}
		switch f.Type {
		case attach.NoticeTree:
			h.syncTree(ctx)
		case attach.NoticeScreenshot:
			h.syncScreenshot(ctx)
		case attach.NoticeStep:
			var n attach.StepNotice
			if f.Decode(&n) == nil {
				h.storeStep(n)
			}
		case attach.NoticeStepGap:
			var n attach.StepGapNotice
			if f.Decode(&n) == nil {
				h.storeGap(n)
			}
		case attach.NoticeStepsDropped, attach.NoticeClosed:
			return true
		}
	}
	return false
}

// resubscribe replaces the subscription on client, resuming after the
// last step line the handle stored, and then refreshes the captures.
func (h *RemoteHandle) resubscribe(parent context.Context, client *attach.Client) bool {
	if !h.subscribeOn(parent, client) {
		return false
	}
	h.syncTree(parent)
	h.syncScreenshot(parent)
	return true
}

func (h *RemoteHandle) subscribeOn(parent context.Context, client *attach.Client) bool {
	ctx, cancel := context.WithTimeout(parent, remoteCallTimeout)
	defer cancel()
	h.mu.Lock()
	runID, seq := h.store.steps.runID, h.store.steps.lastSeq
	h.mu.Unlock()
	return client.Call(ctx, attach.TypeSubscribe, attach.Subscribe{RunID: runID, SinceSeq: seq}, nil) == nil
}

// redial connects again after the connection ended, subscribes on the new
// connection and makes it the handle's connection. The socket path belongs
// to the device, so whoever holds the device next answers on it. A new
// connection is kept only when the process that answers is the one the
// handle attached to, identified by its pid and start time. Any other
// holder ends the handle, so the caller attaches to it as a new viewer
// instead of silently following a different run.
func (h *RemoteHandle) redial(parent context.Context) (*attach.Client, bool) {
	ctx, cancel := context.WithTimeout(parent, remoteCallTimeout)
	defer cancel()
	client, err := h.cfg.dial(ctx)
	if err != nil {
		return nil, false
	}
	if !h.sameHolder(ctx, client) || !h.subscribeOn(parent, client) {
		_ = client.Close()
		return nil, false
	}
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()
		_ = client.Close()
		return nil, false
	}
	old := h.client
	h.client = client
	h.mu.Unlock()
	_ = old.Close()
	h.syncTree(parent)
	h.syncScreenshot(parent)
	return client, true
}

// sameHolder reports whether the process behind client is the holder the
// handle first attached to.
func (h *RemoteHandle) sameHolder(ctx context.Context, client *attach.Client) bool {
	var status attach.Status
	if err := client.Call(ctx, attach.TypeStatus, nil, &status); err != nil {
		return false
	}
	h.mu.Lock()
	want := h.info.Holder
	h.mu.Unlock()
	var wantStarted int64
	if !want.StartedAt.IsZero() {
		wantStarted = want.StartedAt.UnixMilli()
	}
	return status.Holder.PID == want.PID && status.Holder.StartedAt == wantStarted
}

func (h *RemoteHandle) finish() {
	defer close(h.done)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.fanout.endAllLocked(ObservedClosed)
}

func (h *RemoteHandle) syncTree(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, remoteCallTimeout)
	defer cancel()
	_ = h.pullTree(ctx)
}

func (h *RemoteHandle) syncScreenshot(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, remoteCallTimeout)
	defer cancel()
	_ = h.pullScreenshot(ctx)
}

func (h *RemoteHandle) pullTree(ctx context.Context) error {
	var r attach.TreeResult
	if err := h.pull(ctx, attach.TypeTreeLatest, &r); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store.storeTree(r) {
		h.fanout.notifyLocked(Observation{Kind: ObservedTree, Epoch: r.Epoch, Revision: r.Revision, Stable: r.Stable})
	}
	return nil
}

func (h *RemoteHandle) pullScreenshot(ctx context.Context) error {
	var r attach.ScreenshotResult
	if err := h.pull(ctx, attach.TypeScreenshotLatest, &r); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store.storeScreenshot(r) {
		h.fanout.notifyLocked(Observation{Kind: ObservedScreenshot, Epoch: r.Epoch, Revision: r.Revision, Stable: r.Stable})
	}
	return nil
}

// pull sends one request that returns a capture, once the handle's
// single pull slot is free. It gives up when ctx ends first.
func (h *RemoteHandle) pull(ctx context.Context, messageType string, result any) error {
	select {
	case h.pullSlot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-h.pullSlot }()
	return h.currentClient().Call(ctx, messageType, nil, result)
}

func (h *RemoteHandle) storeStep(n attach.StepNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	line, kept := h.store.storeStep(n)
	if !kept {
		return
	}
	h.fanout.notifyLocked(Observation{Kind: ObservedStep, Epoch: n.Epoch, Revision: uint64(line.Seq), Stable: true})
}

func (h *RemoteHandle) storeGap(n attach.StepGapNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store.forgetSteps()
	h.fanout.notifyLocked(Observation{Kind: ObservedStepGap, Revision: uint64(n.OldestSeq), Stable: true})
}
