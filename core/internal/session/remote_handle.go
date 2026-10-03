package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/runfolder"
)

const (
	// remoteCallTimeout bounds one request a handle makes to the holder
	// on its own, such as a status read or a capture pull.
	remoteCallTimeout = 5 * time.Second
	// remotePrimeTimeout bounds the first reads of a new handle: the
	// status, the subscription and the newest tree and screenshot.
	remotePrimeTimeout = 15 * time.Second
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
	// DialTimeout, HelloTimeout and PrimeTimeout bound the connect, the
	// hello exchange and the first reads of DialRemote. Zero selects
	// attach.DefaultDialTimeout, attach.DefaultClientHelloTimeout and
	// remotePrimeTimeout.
	DialTimeout  time.Duration
	HelloTimeout time.Duration
	PrimeTimeout time.Duration
}

func (c RemoteConfig) dial(ctx context.Context) (*attach.Client, error) {
	return attach.Dial(ctx, attach.ClientConfig{Path: c.Path, Client: c.Client, PeerUID: c.PeerUID, NoticeBuffer: c.NoticeBuffer,
		DialTimeout: c.DialTimeout, HelloTimeout: c.HelloTimeout})
}

// RemoteHandle is a Viewer, a StepSource and a Refresher over the attach
// socket of the process that holds a device. It never satisfies
// Controller, because it has no activity gate: the holder alone decides
// when a fresh capture is safe.
//
// Latest serves the newest tree and screenshot the handle pulled from the
// holder, with the holder's own revisions and the tree already masked.
// Paired, Highlightable, Scale and the stable flags are the holder's
// values when it answered the newest pull. Every snapshot carries the
// handle's Generation, which is unique to the handle.
type RemoteHandle struct {
	cfg        RemoteConfig
	base       device.Descriptor
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}

	// pullSlot keeps one capture pull in flight, so the responses a handle
	// holds in memory stay at one large frame. A caller waits for it with
	// its own context, so a slow pull never outlasts the deadline of the
	// caller behind it.
	pullSlot chan struct{}

	mu     sync.Mutex
	client *attach.Client
	info   Info
	store  captureStore
	fanout fanout
	closed bool
}

var (
	_ Viewer     = (*RemoteHandle)(nil)
	_ StepSource = (*RemoteHandle)(nil)
	_ Refresher  = (*RemoteHandle)(nil)
)

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
	}
	h.fanout = newFanout(&h.mu)
	h.pullSlot = make(chan struct{}, 1)
	h.store.generation = h.generation
	h.store.snap.Generation = h.generation
	primeTimeout := cfg.PrimeTimeout
	if primeTimeout <= 0 {
		primeTimeout = remotePrimeTimeout
	}
	pctx, cancelPrime := context.WithTimeout(ctx, primeTimeout)
	defer cancelPrime()
	if err := h.prime(pctx, client); err != nil {
		cancel()
		_ = client.Close()
		if ctx.Err() == nil && pctx.Err() != nil {
			return nil, &attach.TimeoutError{Stage: "prime", Path: cfg.Path, After: primeTimeout}
		}
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
	return h.store.snap
}

// FreshTree asks the holder for a fresh capture. A holder that is
// running or has a call in flight refuses with HOLDER_BUSY, which the
// returned error carries.
func (h *RemoteHandle) FreshTree(ctx context.Context) (Snapshot, error) {
	var result attach.FreshResult
	if err := h.pull(ctx, attach.TypeTreeFresh, &result); err != nil {
		return Snapshot{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store.storeTree(result.Tree) {
		h.fanout.notifyLocked(Observation{Kind: ObservedTree, Epoch: result.Tree.Epoch, Revision: result.Tree.Revision, Stable: result.Tree.Stable})
	}
	if h.store.storeScreenshot(result.Screenshot) {
		h.fanout.notifyLocked(Observation{Kind: ObservedScreenshot, Epoch: result.Screenshot.Epoch, Revision: result.Screenshot.Revision, Stable: result.Screenshot.Stable})
	}
	h.store.snap.Paired, h.store.snap.Highlightable, h.store.snap.Scale = result.Paired, result.Highlightable, result.Scale
	return h.store.snap, nil
}

// StepsSince implements StepSource over the lines the handle received.
func (h *RemoteHandle) StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.store.steps.since(runID, sinceSeq)
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
	return h.fanout.subscribe(ctx, &h.store.steps, func() bool { return h.closed }, ErrRemoteClosed, resume)
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
