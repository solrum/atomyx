package session

import (
	"context"
	"sync"
)

// ActivityGate keeps a fresh capture from interleaving with a tool call.
// Any number of tool calls may be inside the gate at once, because a
// host may run several. A fresh capture holds the gate alone, and it
// never waits for it: TryExclusive refuses while any call is inside or
// another capture holds the gate. A call that arrives while a capture
// holds the gate waits for the capture to end, which is bounded by one
// capture, and honors the caller's context while it waits.
type ActivityGate struct {
	mu        sync.Mutex
	inside    int
	exclusive bool
	waiting   int
	changed   chan struct{}
}

// NewActivityGate returns an open gate.
func NewActivityGate() *ActivityGate {
	return &ActivityGate{changed: make(chan struct{})}
}

// Enter admits one tool call and returns the function that ends it. It
// returns the context error without admitting the call when ctx ends
// first, and a ctx that already ended never enters a free gate.
func (g *ActivityGate) Enter(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.exclusive {
		if err := g.waitLocked(ctx); err != nil {
			g.mu.Unlock()
			return nil, err
		}
	}
	g.inside++
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(g.leave) }, nil
}

// TryExclusive admits the caller alone when no tool call is inside and
// no other caller holds the gate, and reports false at once otherwise.
// The returned function ends the admission.
func (g *ActivityGate) TryExclusive() (release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.exclusive || g.inside > 0 {
		return nil, false
	}
	g.exclusive = true
	var once sync.Once
	return func() { once.Do(g.leaveExclusive) }, true
}

// Waiting reports how many callers are blocked at the gate.
func (g *ActivityGate) Waiting() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waiting
}

// waitLocked blocks until no capture holds the gate or ctx ends. It is
// called and returns with g.mu held.
func (g *ActivityGate) waitLocked(ctx context.Context) error {
	g.waiting++
	defer func() { g.waiting-- }()
	for g.exclusive {
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
			g.mu.Lock()
		case <-ctx.Done():
			g.mu.Lock()
			return ctx.Err()
		}
	}
	return nil
}

func (g *ActivityGate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inside--
}

func (g *ActivityGate) leaveExclusive() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.exclusive = false
	close(g.changed)
	g.changed = make(chan struct{})
}
