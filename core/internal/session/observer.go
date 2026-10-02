package session

import "dev.atomyx/internal/wire"

// Observer receives every capture an observing driver sees. Each value
// carries the action epoch that was active when the capture began, so a
// consumer can tell whether a tree and a screenshot describe the same
// screen: equal epochs mean no mutating call started or finished
// between them.
//
// Calls arrive on the goroutine of the driver call that produced the
// capture, so an implementation must return quickly and must never
// block on a slow consumer. Only successful captures are reported.
//
// A capture is stable when no mutating call was in flight as it began
// and none started or finished before it returned. An unstable capture
// is still reported, because it is the newest thing the device showed,
// but its epoch proves nothing, so a consumer must never treat it as
// half of a matched pair.
type Observer interface {
	ObserveTree(tree wire.TreeNodeWire, epoch uint64, stable bool)
	ObserveScreenshot(image []byte, epoch uint64, stable bool)
	ObserveScreenSize(size wire.Size, epoch uint64, stable bool)
}

type noopObserver struct{}

func (noopObserver) ObserveTree(wire.TreeNodeWire, uint64, bool) {}
func (noopObserver) ObserveScreenshot([]byte, uint64, bool)      {}
func (noopObserver) ObserveScreenSize(wire.Size, uint64, bool)   {}
