package session

import (
	"context"
	"sync"
)

// subscriber is one consumer of a fanout.
type subscriber struct {
	ch   chan Observation
	done chan struct{}
	// steps is true for a subscriber that asked for step notices through
	// SubscribeSince.
	steps bool
}

// fanout delivers observations to subscribers without ever blocking the
// publisher. A subscriber that leaves subscriberBuffer notifications
// unread is ended with ObservedDropped. Its owner supplies the lock that
// guards the subscriber set, so a publish and the state it announces stay
// atomic. Every method named Locked requires that lock to be held.
type fanout struct {
	mu   sync.Locker
	subs map[*subscriber]struct{}
}

func newFanout(mu sync.Locker) fanout {
	return fanout{mu: mu, subs: make(map[*subscriber]struct{})}
}

// subscribe registers a subscriber. A non-nil resume makes it a step
// subscriber, and the notices that replay ring are queued before any live
// one. closed runs under the lock, and an owner that reports true refuses
// with errClosed.
func (f *fanout) subscribe(ctx context.Context, ring *stepRing, closed func() bool, errClosed error, resume *stepResume) (<-chan Observation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sub := &subscriber{ch: make(chan Observation, subscriberBuffer+1), done: make(chan struct{}), steps: resume != nil}
	f.mu.Lock()
	if closed() {
		f.mu.Unlock()
		return nil, errClosed
	}
	if resume != nil {
		notices, err := ring.resumeLocked(resume.runID, resume.sinceSeq)
		if err != nil {
			f.mu.Unlock()
			return nil, err
		}
		for _, o := range notices {
			sub.ch <- o
		}
	}
	f.subs[sub] = struct{}{}
	f.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			f.dropLocked(sub)
			f.mu.Unlock()
		case <-sub.done:
		}
	}()
	return sub.ch, nil
}

func (f *fanout) notifyLocked(o Observation) {
	for sub := range f.subs {
		if isStepNotice(o.Kind) && !sub.steps {
			continue
		}
		if len(sub.ch) >= subscriberBuffer {
			f.endLocked(sub, ObservedDropped)
			continue
		}
		sub.ch <- o
	}
}

// endAllLocked ends every subscriber with kind.
func (f *fanout) endAllLocked(kind ObservationKind) {
	for sub := range f.subs {
		f.endLocked(sub, kind)
	}
}

// endLocked sends the terminal notification and then drops sub. Ordinary
// notifications stop at subscriberBuffer, so the slot is always free.
func (f *fanout) endLocked(sub *subscriber, kind ObservationKind) {
	sub.ch <- Observation{Kind: kind}
	f.dropLocked(sub)
}

func (f *fanout) dropLocked(sub *subscriber) {
	if _, live := f.subs[sub]; !live {
		return
	}
	delete(f.subs, sub)
	close(sub.ch)
	close(sub.done)
}
