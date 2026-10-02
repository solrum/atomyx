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
type Observer interface {
	ObserveTree(tree wire.TreeNodeWire, epoch uint64)
	ObserveScreenshot(image []byte, epoch uint64)
	ObserveScreenSize(size wire.Size, epoch uint64)
}

type noopObserver struct{}

func (noopObserver) ObserveTree(wire.TreeNodeWire, uint64) {}
func (noopObserver) ObserveScreenshot([]byte, uint64)      {}
func (noopObserver) ObserveScreenSize(wire.Size, uint64)   {}
