package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg" // registers the JPEG header decoder
	_ "image/png"  // registers the PNG header decoder
	"math"
	"sync"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

// subscriberBuffer is how many capture notifications a subscriber may
// leave unread before the hub drops it. Its channel holds one more, the
// slot the terminal notification always finds free.
const subscriberBuffer = 16

// aspectTolerance is how far the aspect ratio of the screenshot and of
// the reported screen size may differ, as a fraction, before the two are
// taken to describe different orientations. It absorbs the rounding of a
// scaled image.
const aspectTolerance = 0.02

// ErrHubClosed reports a subscription to a hub that was closed.
var ErrHubClosed = errors.New("the capture hub is closed")

// Hub is the Observer that keeps the newest tree, screenshot and screen
// size with their epochs, and tells subscribers when one changes. It
// never calls the device. A subscriber that does not keep up is dropped
// instead of slowing the capture that published.
type Hub struct {
	mu     sync.Mutex
	closed bool
	fanout fanout

	droppedInterventions int

	tree       wire.TreeNodeWire
	treeEpoch  uint64
	treeSeq    uint64
	treeStable bool
	hasTree    bool

	maskedSeq  uint64
	maskedTree wire.TreeNodeWire

	shot       []byte
	shotEpoch  uint64
	shotSeq    uint64
	shotStable bool
	shotFormat ImageFormat
	shotWidth  int
	shotHeight int
	hasShot    bool

	size       wire.Size
	sizeEpoch  uint64
	sizeSeq    uint64
	sizeStable bool
	hasSize    bool

	steps stepRing
}

var _ Observer = (*Hub)(nil)

// NewHub returns an empty hub.
func NewHub() *Hub {
	h := &Hub{}
	h.fanout = newFanout(&h.mu)
	return h
}

// ObserveTree implements Observer.
func (h *Hub) ObserveTree(tree wire.TreeNodeWire, epoch uint64, stable bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tree, h.treeEpoch, h.treeStable, h.hasTree = tree, epoch, stable, true
	h.treeSeq++
	h.fanout.notifyLocked(Observation{Kind: ObservedTree, Epoch: epoch, Revision: h.treeSeq, Stable: stable})
}

// ObserveScreenshot implements Observer.
func (h *Hub) ObserveScreenshot(img []byte, epoch uint64, stable bool) {
	format, width, height := sniffImage(img)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shot, h.shotEpoch, h.shotStable, h.hasShot = img, epoch, stable, true
	h.shotFormat, h.shotWidth, h.shotHeight = format, width, height
	h.shotSeq++
	h.fanout.notifyLocked(Observation{Kind: ObservedScreenshot, Epoch: epoch, Revision: h.shotSeq, Stable: stable})
}

// ObserveScreenSize implements Observer.
func (h *Hub) ObserveScreenSize(size wire.Size, epoch uint64, stable bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.size, h.sizeEpoch, h.sizeStable, h.hasSize = size, epoch, stable, true
	h.sizeSeq++
	h.fanout.notifyLocked(Observation{Kind: ObservedScreenSize, Epoch: epoch, Revision: h.sizeSeq, Stable: stable})
}

// PublishState tells every subscriber that the holder moved to state. pause
// says where the run stands and is non-nil only for the paused state. It
// never blocks on a subscriber, and one that is too far behind is dropped
// like any other.
func (h *Hub) PublishState(state HolderState, command, runID string, pause *PauseLocation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fanout.notifyLocked(Observation{Kind: ObservedState, Stable: true, State: state, Command: command, RunID: runID, Pause: pause, DroppedInterventions: h.droppedInterventions})
}

// SetDroppedInterventions records how many intervention lines the run folder
// could not keep. The next PublishState reports it, so a viewer learns of
// the loss at the next state change.
func (h *Hub) SetDroppedInterventions(n int) {
	h.mu.Lock()
	h.droppedInterventions = n
	h.mu.Unlock()
}

// Latest returns the newest captures. The masked copy of a tree is made
// once, on the first read after the tree was stored, and shared by every
// later read of that tree.
func (h *Hub) Latest() Snapshot {
	h.mu.Lock()
	snap := Snapshot{
		HasTree: h.hasTree, TreeEpoch: h.treeEpoch, TreeRevision: h.treeSeq,
		Screenshot: h.shot, HasScreenshot: h.hasShot, ScreenshotFormat: h.shotFormat,
		ScreenshotEpoch: h.shotEpoch, ScreenshotRevision: h.shotSeq,
		TreeStable: h.hasTree && h.treeStable, ScreenshotStable: h.hasShot && h.shotStable,
	}
	snap.Paired, snap.Highlightable, snap.Scale = h.pairingLocked()
	if !h.hasTree {
		h.mu.Unlock()
		return snap
	}
	if h.maskedSeq == h.treeSeq {
		snap.Tree = h.maskedTree
		h.mu.Unlock()
		return snap
	}
	seq, tree := h.treeSeq, h.tree
	h.mu.Unlock()

	masked := orchestra.MaskSecure(tree)
	h.mu.Lock()
	if h.treeSeq == seq {
		h.maskedSeq, h.maskedTree = seq, masked
	}
	h.mu.Unlock()
	snap.Tree = masked
	return snap
}

// pairingLocked reports whether the newest tree and screenshot describe
// the same screen, whether a node's bounds can be drawn on the screenshot,
// and the scale between them.
func (h *Hub) pairingLocked() (paired, highlightable bool, scale float64) {
	if h.sizeMatchesShotLocked() {
		scale = float64(h.shotWidth) / h.size.Width
	}
	paired = h.hasTree && h.hasShot && h.treeStable && h.shotStable && h.treeEpoch == h.shotEpoch
	return paired, paired && scale > 0, scale
}

// pairing is pairingLocked for a caller that holds no lock.
func (h *Hub) pairing() (paired, highlightable bool, scale float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pairingLocked()
}

// sizeMatchesShotLocked reports whether the newest screen size can scale
// the newest screenshot. The size may have been read before the device
// rotated, so it counts only when it is stable and has the aspect ratio
// of the screenshot. A rotated device flips the ratio, and a stale size
// would then draw every highlight in the wrong place.
func (h *Hub) sizeMatchesShotLocked() bool {
	if !h.hasShot || !h.hasSize || !h.sizeStable {
		return false
	}
	if h.shotWidth <= 0 || h.shotHeight <= 0 || h.size.Width <= 0 || h.size.Height <= 0 {
		return false
	}
	shot := float64(h.shotWidth) / float64(h.shotHeight)
	screen := h.size.Width / h.size.Height
	return math.Abs(shot-screen) <= aspectTolerance*screen
}

// sizeCurrent reports whether a stable screen size was read in the epoch
// of the newest tree.
func (h *Hub) sizeCurrent() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hasSize && h.sizeStable && h.hasTree && h.sizeEpoch == h.treeEpoch
}

// Subscribe reports every capture until ctx ends, the hub closes, or
// the subscriber leaves subscriberBuffer notifications unread. A hub
// that ends the subscription sends ObservedClosed or ObservedDropped as
// the last notification and then closes the channel, so the subscriber
// can tell a closed session from falling behind, and re-reads Latest
// only in the second case. Ending through ctx sends nothing.
func (h *Hub) Subscribe(ctx context.Context) (<-chan Observation, error) {
	return h.subscribe(ctx, nil)
}

// stepResume names the last step line a step subscriber read.
type stepResume struct {
	runID    string
	sinceSeq int64
}

// subscribe registers a subscriber. A non-nil resume makes it a step
// subscriber, and its resume notices are queued before any live one.
func (h *Hub) subscribe(ctx context.Context, resume *stepResume) (<-chan Observation, error) {
	return h.fanout.subscribe(ctx, &h.steps, func() bool { return h.closed }, ErrHubClosed, resume)
}

// Close ends every subscription. Later captures are still stored.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.fanout.endAllLocked(ObservedClosed)
}

// sniffImage reads the format and pixel size from the image header
// without decoding the pixels. It reports ImageUnknown and zeros for
// bytes that are neither a PNG nor a JPEG.
func sniffImage(img []byte) (format ImageFormat, width, height int) {
	cfg, name, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return ImageUnknown, 0, 0
	}
	switch name {
	case "png":
		return ImagePNG, cfg.Width, cfg.Height
	case "jpeg":
		return ImageJPEG, cfg.Width, cfg.Height
	}
	return ImageUnknown, 0, 0
}

// currentEpoch reports the newest action epoch any capture was stored in.
func (h *Hub) currentEpoch() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return max(h.treeEpoch, h.shotEpoch, h.sizeEpoch)
}
