package mcp

import (
	"context"
	"sync"
)

// transitionLock is a mutex whose waiters can give up when their
// context ends, so a caller that must not outlast its deadline, such as
// a disconnect, never queues behind a slow transition indefinitely. The
// zero value is an unlocked lock.
type transitionLock struct {
	once sync.Once
	slot chan struct{}
}

func (l *transitionLock) init() { l.once.Do(func() { l.slot = make(chan struct{}, 1) }) }

// Lock blocks until the lock is held.
func (l *transitionLock) Lock() {
	l.init()
	l.slot <- struct{}{}
}

// TryLock takes the lock when it is free and reports whether it did.
func (l *transitionLock) TryLock() bool {
	l.init()
	select {
	case l.slot <- struct{}{}:
		return true
	default:
		return false
	}
}

// LockContext blocks until the lock is held or ctx ends. It returns the
// context error without taking the lock when ctx ended first, and a ctx
// that already ended never takes a free lock.
func (l *transitionLock) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.init()
	select {
	case l.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlock releases the lock.
func (l *transitionLock) Unlock() { <-l.slot }
