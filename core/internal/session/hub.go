package session

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg" // registers the JPEG header decoder
	_ "image/png"  // registers the PNG header decoder
	"sync"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

// subscriberBuffer is how many notifications a subscriber may leave
// unread before the hub drops it.
const subscriberBuffer = 16

// ErrHubClosed reports a subscription to a hub that was closed.
var ErrHubClosed = errors.New("the capture hub is closed")

// Hub is the Observer that keeps the newest tree, screenshot and screen
// size with their epochs, and tells subscribers when one changes. It
// never calls the device. A subscriber that does not keep up is dropped
// instead of slowing the capture that published.
type Hub struct {
	mu     sync.Mutex
	closed bool
	subs   map[*subscriber]struct{}

	tree      wire.TreeNodeWire
	treeEpoch uint64
	treeSeq   uint64
	hasTree   bool

	maskedSeq  uint64
	maskedTree wire.TreeNodeWire

	shot       []byte
	shotEpoch  uint64
	shotFormat ImageFormat
	shotWidth  int
	hasShot    bool

	size      wire.Size
	sizeEpoch uint64
	hasSize   bool
}

var _ Observer = (*Hub)(nil)

type subscriber struct {
	ch   chan Observation
	done chan struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// ObserveTree implements Observer.
func (h *Hub) ObserveTree(tree wire.TreeNodeWire, epoch uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tree, h.treeEpoch, h.hasTree = tree, epoch, true
	h.treeSeq++
	h.notifyLocked(Observation{Kind: ObservedTree, Epoch: epoch})
}

// ObserveScreenshot implements Observer.
func (h *Hub) ObserveScreenshot(img []byte, epoch uint64) {
	format, width := sniffImage(img)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shot, h.shotEpoch, h.hasShot = img, epoch, true
	h.shotFormat, h.shotWidth = format, width
	h.notifyLocked(Observation{Kind: ObservedScreenshot, Epoch: epoch})
}

// ObserveScreenSize implements Observer.
func (h *Hub) ObserveScreenSize(size wire.Size, epoch uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.size, h.sizeEpoch, h.hasSize = size, epoch, true
	h.notifyLocked(Observation{Kind: ObservedScreenSize, Epoch: epoch})
}

// Latest returns the newest captures. The masked copy of a tree is made
// once, on the first read after the tree was stored, and shared by every
// later read of that tree.
func (h *Hub) Latest() Snapshot {
	h.mu.Lock()
	snap := Snapshot{
		HasTree: h.hasTree, TreeEpoch: h.treeEpoch,
		Screenshot: h.shot, HasScreenshot: h.hasShot, ScreenshotFormat: h.shotFormat, ScreenshotEpoch: h.shotEpoch,
	}
	if h.hasShot && h.hasSize && h.shotWidth > 0 && h.size.Width > 0 {
		snap.Scale = float64(h.shotWidth) / h.size.Width
	}
	snap.Paired = h.hasTree && h.hasShot && h.treeEpoch == h.shotEpoch
	snap.Highlightable = snap.Paired && snap.Scale > 0
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

// Subscribe reports every capture until ctx ends, the hub closes, or
// the subscriber leaves subscriberBuffer notifications unread. The hub
// then closes the channel, and the subscriber re-reads Latest.
func (h *Hub) Subscribe(ctx context.Context) (<-chan Observation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sub := &subscriber{ch: make(chan Observation, subscriberBuffer), done: make(chan struct{})}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrHubClosed
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

// Close ends every subscription. Later captures are still stored.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subs {
		h.dropLocked(sub)
	}
}

func (h *Hub) notifyLocked(o Observation) {
	for sub := range h.subs {
		select {
		case sub.ch <- o:
		default:
			h.dropLocked(sub)
		}
	}
}

func (h *Hub) dropLocked(sub *subscriber) {
	if _, live := h.subs[sub]; !live {
		return
	}
	delete(h.subs, sub)
	close(sub.ch)
	close(sub.done)
}

// sniffImage reads the format and pixel width from the image header
// without decoding the pixels. It reports ImageUnknown and zero for
// bytes that are neither a PNG nor a JPEG.
func sniffImage(img []byte) (ImageFormat, int) {
	cfg, name, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return ImageUnknown, 0
	}
	switch name {
	case "png":
		return ImagePNG, cfg.Width
	case "jpeg":
		return ImageJPEG, cfg.Width
	}
	return ImageUnknown, 0
}
