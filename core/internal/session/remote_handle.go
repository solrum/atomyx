package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/wire"
)

const (
	// remoteCallTimeout bounds one request a handle makes to the holder
	// on its own, such as a status read or a capture pull.
	remoteCallTimeout = 5 * time.Second
	// pullBusyRetries is how many times a capture pull is repeated when
	// the holder answers that the previous pull response is still being
	// written. That answer is a transient overlap, never a refusal.
	pullBusyRetries = 3
)

// ErrRemoteClosed reports a subscription to a handle whose connection to
// the holder has ended.
var ErrRemoteClosed = errors.New("the connection to the holder is closed")

// remoteGenerations numbers the handles this process has attached, so
// two handles never share a Generation.
var remoteGenerations atomic.Uint64

// RemoteConfig says how a handle reaches a holder.
type RemoteConfig struct {
	// Path is the holder's socket path.
	Path string
	// Client names the viewer in the hello request.
	Client string
	// PeerUID reads the uid of the holder. The platform lookup is the
	// default.
	PeerUID attach.PeerCredFunc
	// NoticeBuffer bounds the step notices the connection holds for the
	// handle. Zero selects attach.DefaultNoticeBuffer.
	NoticeBuffer int
}

func (c RemoteConfig) dial(ctx context.Context) (*attach.Client, error) {
	return attach.Dial(ctx, attach.ClientConfig{Path: c.Path, Client: c.Client, PeerUID: c.PeerUID, NoticeBuffer: c.NoticeBuffer})
}

// RemoteHandle is a Viewer, a StepSource and a Refresher over the attach
// socket of the process that holds a device. It never satisfies
// Controller, because it has no activity gate: the holder alone decides
// when a fresh capture is safe.
//
// Latest serves the newest tree and screenshot the handle pulled from the
// holder, with the holder's own revisions and the tree already masked.
// Paired, Highlightable and Scale are known only for the capture a
// FreshTree returned, because a notice does not say whether a mutating
// call overlapped a capture. Every snapshot carries the handle's
// Generation, which is unique to the handle.
type RemoteHandle struct {
	cfg        RemoteConfig
	base       device.Descriptor
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}

	// pullMu keeps one capture pull in flight, because the holder answers
	// a second pull on the same connection with HOLDER_BUSY.
	pullMu sync.Mutex

	mu     sync.Mutex
	client *attach.Client
	info   Info
	snap   Snapshot
	fresh  freshStamp
	steps  stepRing
	subs   map[*subscriber]struct{}
	closed bool
}

var (
	_ Viewer     = (*RemoteHandle)(nil)
	_ StepSource = (*RemoteHandle)(nil)
	_ Refresher  = (*RemoteHandle)(nil)
)

// freshStamp is what a fresh capture reported about the pair it returned.
type freshStamp struct {
	valid         bool
	treeRevision  uint64
	shotRevision  uint64
	paired        bool
	highlightable bool
	scale         float64
}

// DialRemote connects to the holder at cfg.Path, reads its status and its
// newest captures, and starts following it. desc supplies the descriptor
// fields the holder does not report.
func DialRemote(ctx context.Context, cfg RemoteConfig, desc device.Descriptor) (*RemoteHandle, error) {
	client, err := cfg.dial(ctx)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	h := &RemoteHandle{
		cfg:        cfg,
		base:       desc,
		generation: remoteGenerations.Add(1),
		ctx:        hctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		client:     client,
		subs:       make(map[*subscriber]struct{}),
	}
	h.snap.Generation = h.generation
	if err := h.prime(ctx, client); err != nil {
		cancel()
		_ = client.Close()
		return nil, err
	}
	go h.pump(hctx)
	return h, nil
}

func (h *RemoteHandle) prime(ctx context.Context, client *attach.Client) error {
	var status attach.Status
	if err := client.Call(ctx, attach.TypeStatus, nil, &status); err != nil {
		return err
	}
	if err := client.Call(ctx, attach.TypeSubscribe, attach.Subscribe{}, nil); err != nil {
		return err
	}
	h.mu.Lock()
	h.info = h.infoFrom(status)
	h.mu.Unlock()
	if err := h.pullTree(ctx); err != nil {
		return err
	}
	return h.pullScreenshot(ctx)
}

func (h *RemoteHandle) infoFrom(s attach.Status) Info {
	d := h.base
	d.ID, d.Name, d.Platform = s.Device.ID, s.Device.Name, device.Platform(s.Device.Platform)
	holder := lease.Holder{
		PID:     s.Holder.PID,
		Command: s.Holder.Command,
		Role:    s.Holder.Role,
		RunID:   s.Holder.RunID,
		Socket:  h.cfg.Path,
	}
	if s.Holder.StartedAt > 0 {
		holder.StartedAt = time.UnixMilli(s.Holder.StartedAt).UTC()
	}
	return Info{Device: d, Holder: holder, State: HolderState(s.State), Generation: h.generation}
}

// Info asks the holder for its status. When the holder cannot answer, it
// returns the last status read, with the state closed once the connection
// has ended.
func (h *RemoteHandle) Info() Info {
	h.mu.Lock()
	client, info, closed := h.client, h.info, h.closed
	h.mu.Unlock()
	if closed {
		info.State = StateClosed
		return info
	}
	ctx, cancel := context.WithTimeout(h.ctx, remoteCallTimeout)
	defer cancel()
	var status attach.Status
	if err := client.Call(ctx, attach.TypeStatus, nil, &status); err != nil {
		if client.Err() != nil {
			info.State = StateClosed
		}
		return info
	}
	info = h.infoFrom(status)
	h.mu.Lock()
	h.info = info
	h.mu.Unlock()
	return info
}

// Latest returns the newest captures the handle has pulled.
func (h *RemoteHandle) Latest() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}

// FreshTree asks the holder for a fresh capture. A holder that is
// running or has a call in flight refuses with HOLDER_BUSY, which the
// returned error carries.
func (h *RemoteHandle) FreshTree(ctx context.Context) (Snapshot, error) {
	h.pullMu.Lock()
	client := h.currentClient()
	var result attach.FreshResult
	err := client.Call(ctx, attach.TypeTreeFresh, nil, &result)
	h.pullMu.Unlock()
	if err != nil {
		return Snapshot{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.storeTreeLocked(result.Tree)
	h.storeScreenshotLocked(result.Screenshot)
	h.fresh = freshStamp{
		valid:         true,
		treeRevision:  result.Tree.Revision,
		shotRevision:  result.Screenshot.Revision,
		paired:        result.Paired,
		highlightable: result.Highlightable,
		scale:         result.Scale,
	}
	h.deriveLocked()
	return h.snap, nil
}

// StepsSince implements StepSource over the lines the handle received.
func (h *RemoteHandle) StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.steps.since(runID, sinceSeq)
}

// Subscribe implements Viewer.
func (h *RemoteHandle) Subscribe(ctx context.Context) (<-chan Observation, error) {
	return h.subscribe(ctx, nil)
}

// SubscribeSince implements StepSource.
func (h *RemoteHandle) SubscribeSince(ctx context.Context, runID string, sinceSeq int64) (<-chan Observation, error) {
	return h.subscribe(ctx, &stepResume{runID: runID, sinceSeq: sinceSeq})
}

func (h *RemoteHandle) subscribe(ctx context.Context, resume *stepResume) (<-chan Observation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sub := &subscriber{ch: make(chan Observation, subscriberBuffer+1), done: make(chan struct{}), steps: resume != nil}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrRemoteClosed
	}
	if resume != nil {
		notices, err := h.steps.resumeLocked(resume.runID, resume.sinceSeq)
		if err != nil {
			h.mu.Unlock()
			return nil, err
		}
		for _, o := range notices {
			sub.ch <- o
		}
	}
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			h.dropLocked(sub)
			h.mu.Unlock()
		case <-sub.done:
		}
	}()
	return sub.ch, nil
}

// Close ends the connection and every subscription, and waits for the
// handle's goroutines to stop. It is safe to call more than once.
func (h *RemoteHandle) Close() error {
	h.cancel()
	_ = h.currentClient().Close()
	<-h.done
	return nil
}

// Done is closed when the connection to the holder has ended.
func (h *RemoteHandle) Done() <-chan struct{} { return h.done }

func (h *RemoteHandle) currentClient() *attach.Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.client
}

// pump follows the holder's notices until the holder is gone. When a
// subscription ends while the holder lives, the pump reconnects and
// resumes after the last step line it stored.
func (h *RemoteHandle) pump(ctx context.Context) {
	defer h.finish()
	for h.consume(ctx, h.currentClient()) {
		if !h.resubscribe(ctx) {
			return
		}
	}
}

// consume handles notices until the connection ends, and reports whether
// the subscription must be started again. That is the case when the
// client dropped step notices, and when the holder ended the subscription,
// which it does for a viewer that fell behind as well as for a holder that
// is closing. Starting again fails when the holder is gone.
func (h *RemoteHandle) consume(ctx context.Context, client *attach.Client) (resubscribe bool) {
	for f := range client.Notices() {
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

func (h *RemoteHandle) resubscribe(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, remoteCallTimeout)
	defer cancel()
	client, err := h.cfg.dial(ctx)
	if err != nil {
		return false
	}
	h.mu.Lock()
	runID, seq := h.steps.runID, h.steps.lastSeq
	h.mu.Unlock()
	if err := client.Call(ctx, attach.TypeSubscribe, attach.Subscribe{RunID: runID, SinceSeq: seq}, nil); err != nil {
		_ = client.Close()
		return false
	}
	h.mu.Lock()
	if h.ctx.Err() != nil {
		h.mu.Unlock()
		_ = client.Close()
		return false
	}
	old := h.client
	h.client = client
	h.mu.Unlock()
	_ = old.Close()
	h.syncTree(parent)
	h.syncScreenshot(parent)
	return true
}

func (h *RemoteHandle) finish() {
	defer close(h.done)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subs {
		h.endLocked(sub, ObservedClosed)
	}
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
	if h.storeTreeLocked(r) {
		h.notifyLocked(Observation{Kind: ObservedTree, Epoch: r.Epoch, Revision: r.Revision, Stable: true})
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
	if h.storeScreenshotLocked(r) {
		h.notifyLocked(Observation{Kind: ObservedScreenshot, Epoch: r.Epoch, Revision: r.Revision, Stable: true})
	}
	return nil
}

func (h *RemoteHandle) pull(ctx context.Context, messageType string, result any) error {
	h.pullMu.Lock()
	defer h.pullMu.Unlock()
	client := h.currentClient()
	var err error
	for range pullBusyRetries + 1 {
		if err = client.Call(ctx, messageType, nil, result); diagCode(err) != diag.CodeHolderBusy {
			return err
		}
	}
	return err
}

func diagCode(err error) string {
	if err == nil {
		return ""
	}
	code, _, _ := diag.Describe(err)
	return code
}

// storeTreeLocked keeps r when it is newer than the held tree and reports
// whether it did.
func (h *RemoteHandle) storeTreeLocked(r attach.TreeResult) bool {
	if r.Revision <= h.snap.TreeRevision {
		return false
	}
	h.snap.HasTree = r.Tree != nil
	h.snap.Tree = wire.TreeNodeWire{}
	if r.Tree != nil {
		h.snap.Tree = *r.Tree
	}
	h.snap.TreeEpoch, h.snap.TreeRevision = r.Epoch, r.Revision
	h.snap.Generation = h.generation
	h.deriveLocked()
	return true
}

func (h *RemoteHandle) storeScreenshotLocked(r attach.ScreenshotResult) bool {
	if r.Revision <= h.snap.ScreenshotRevision {
		return false
	}
	h.snap.HasScreenshot = len(r.Image) > 0
	h.snap.Screenshot = r.Image
	h.snap.ScreenshotFormat = ImageFormat(r.Format)
	h.snap.ScreenshotEpoch, h.snap.ScreenshotRevision = r.Epoch, r.Revision
	h.snap.Generation = h.generation
	h.deriveLocked()
	return true
}

// deriveLocked sets the fields a notice cannot supply. They hold only
// while the held pair is the pair the last fresh capture described.
func (h *RemoteHandle) deriveLocked() {
	f, s := h.fresh, &h.snap
	if f.valid && f.treeRevision == s.TreeRevision && f.shotRevision == s.ScreenshotRevision {
		s.Paired, s.Highlightable, s.Scale = f.paired, f.highlightable, f.scale
		return
	}
	s.Paired, s.Highlightable, s.Scale = false, false, 0
}

func (h *RemoteHandle) storeStep(n attach.StepNotice) {
	line := runfolder.StepLine{
		SchemaVersion: n.Line.SchemaVersion,
		RunID:         n.Line.RunID,
		Seq:           n.Line.Seq,
		StepIndex:     n.Line.StepIndex,
		Command:       n.Line.Command,
		Phase:         runfolder.StepPhase(n.Line.Phase),
		Ok:            n.Line.Ok,
		DurationMs:    n.Line.DurationMs,
		Detail:        n.Line.Detail,
		Artifacts:     n.Line.Artifacts,
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps.push(line)
	h.notifyLocked(Observation{Kind: ObservedStep, Epoch: n.Epoch, Revision: uint64(line.Seq), Stable: true})
}

// storeGap forgets the held lines, because the lines that follow a gap
// are not contiguous with them.
func (h *RemoteHandle) storeGap(n attach.StepGapNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps = stepRing{}
	h.notifyLocked(Observation{Kind: ObservedStepGap, Revision: uint64(n.OldestSeq), Stable: true})
}

func (h *RemoteHandle) notifyLocked(o Observation) {
	for sub := range h.subs {
		if isStepNotice(o.Kind) && !sub.steps {
			continue
		}
		if len(sub.ch) >= subscriberBuffer {
			h.endLocked(sub, ObservedDropped)
			continue
		}
		sub.ch <- o
	}
}

// endLocked sends the terminal notification and then drops sub. Ordinary
// notifications stop at subscriberBuffer, so the slot is always free.
func (h *RemoteHandle) endLocked(sub *subscriber, kind ObservationKind) {
	sub.ch <- Observation{Kind: kind}
	h.dropLocked(sub)
}

func (h *RemoteHandle) dropLocked(sub *subscriber) {
	if _, live := h.subs[sub]; !live {
		return
	}
	delete(h.subs, sub)
	close(sub.ch)
	close(sub.done)
}
