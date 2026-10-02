package session

import (
	"context"
	"sync"
)

// ActivityGate keeps a fresh capture from interleaving with a tool call.
// Any number of tool calls may be inside the gate at once, because a
// host may run several, but a fresh capture holds it alone: it waits
// for the calls inside to end, and calls that arrive meanwhile wait for
// it. Waiting honors the caller's context.
type ActivityGate struct {
	mu           sync.Mutex
	inside       int
	exclusive    bool
	waiting      int
	waitingAlone int
	changed      chan struct{}
}

// NewActivityGate returns an open gate.
func NewActivityGate() *ActivityGate {
	return &ActivityGate{changed: make(chan struct{})}
}

// Enter admits one tool call and returns the function that ends it. It
// returns the context error without admitting the call when ctx ends
// first, and a ctx that already ended never enters a free gate.
func (g *ActivityGate) Enter(ctx context.Context) (release func(), err error) {
	return g.acquire(ctx, false)
}

// Exclusive admits the caller alone and returns the function that ends
// it. It waits for every call inside to end, and holds back later calls
// while it waits, so a steady stream of calls cannot starve it.
func (g *ActivityGate) Exclusive(ctx context.Context) (release func(), err error) {
	return g.acquire(ctx, true)
}

// Waiting reports how many callers are blocked at the gate.
func (g *ActivityGate) Waiting() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waiting
}

func (g *ActivityGate) acquire(ctx context.Context, alone bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if !g.admitsLocked(alone) {
		if err := g.waitLocked(ctx, alone); err != nil {
			g.mu.Unlock()
			return nil, err
		}
	}
	if alone {
		g.exclusive = true
	} else {
		g.inside++
	}
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { g.leave(alone) }) }, nil
}

// waitLocked blocks until the caller is admitted or ctx ends. It is
// called and returns with g.mu held.
func (g *ActivityGate) waitLocked(ctx context.Context, alone bool) error {
	g.waiting++
	if alone {
		g.waitingAlone++
	}
	defer func() {
		g.waiting--
		if alone {
			g.waitingAlone--
		}
	}()
	for !g.admitsLocked(alone) {
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
			g.mu.Lock()
		case <-ctx.Done():
			g.mu.Lock()
			g.broadcastLocked()
			return ctx.Err()
		}
	}
	return nil
}

// admitsLocked reports whether a caller may enter now. A shared caller
// yields to a waiting exclusive one.
func (g *ActivityGate) admitsLocked(alone bool) bool {
	switch {
	case g.exclusive:
		return false
	case alone:
		return g.inside == 0
	default:
		return g.waitingAlone == 0
	}
}

func (g *ActivityGate) leave(alone bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if alone {
		g.exclusive = false
	} else {
		g.inside--
	}
	g.broadcastLocked()
}

func (g *ActivityGate) broadcastLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}
