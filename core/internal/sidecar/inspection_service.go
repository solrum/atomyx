package sidecar

import (
	"context"
	"sync"
	"time"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

// inspectionCacheTTLMs is how long a captured UI tree is served from
// cache before a fresh capture is forced, matching the public
// contract documented for getUiTree.
const inspectionCacheTTLMs = 2000

// inspectionService performs read-only introspection of the selected
// device: UI tree capture (cached for a short TTL to avoid thrashing
// the device on tight polling) and screenshots. Any action that
// changes the screen must call Invalidate to drop a stale cached
// tree.
type inspectionService struct {
	session *Session

	mu         sync.Mutex
	cachedTree *wire.TreeNodeWire
	capturedAt int64
}

func newInspectionService(session *Session) *inspectionService {
	return &inspectionService{session: session}
}

// GetUiTree returns the selected device's UI tree for display, with the
// text and value of every secure node masked. The cache keeps the real
// tree and masking happens on the way out.
func (s *inspectionService) GetUiTree(ctx context.Context, fresh bool) (wire.TreeNodeWire, error) {
	now := time.Now().UnixMilli()
	if !fresh {
		if tree, ok := s.cachedIfFresh(now); ok {
			return orchestra.MaskSecure(tree), nil
		}
	}

	selection, err := requireDeviceOrWireError(s.session)
	if err != nil {
		return wire.TreeNodeWire{}, err
	}
	tree, err := selection.Driver.Hierarchy(ctx)
	if err != nil {
		return wire.TreeNodeWire{}, err
	}

	s.mu.Lock()
	s.cachedTree = &tree
	s.capturedAt = now
	s.mu.Unlock()
	return orchestra.MaskSecure(tree), nil
}

func (s *inspectionService) Screenshot(ctx context.Context) ([]byte, error) {
	selection, err := requireDeviceOrWireError(s.session)
	if err != nil {
		return nil, err
	}
	return selection.Driver.Screenshot(ctx)
}

func (s *inspectionService) Invalidate() {
	s.mu.Lock()
	s.cachedTree = nil
	s.mu.Unlock()
}

func (s *inspectionService) cachedIfFresh(now int64) (wire.TreeNodeWire, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedTree == nil || now-s.capturedAt >= inspectionCacheTTLMs {
		return wire.TreeNodeWire{}, false
	}
	return *s.cachedTree, true
}
