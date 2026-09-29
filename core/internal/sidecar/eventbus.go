package sidecar

import "sync"

// Listener receives every Event published on an EventBus after it
// subscribes.
type Listener func(Event)

// EventBus is in-process pub/sub for sidecar events. Handlers
// publish through the bus; the transport subscribes once and
// forwards each event over the wire. Keeping handlers ignorant of
// the transport lets a test attach a collecting Listener without
// touching stdio.
//
// EventBus is safe for concurrent use.
type EventBus struct {
	mu        sync.Mutex
	listeners map[int]Listener
	nextID    int
}

// NewEventBus constructs an EventBus with no subscribers.
func NewEventBus() *EventBus {
	return &EventBus{listeners: make(map[int]Listener)}
}

// Subscribe registers listener and returns a function that removes
// it. Calling the returned function more than once is a no-op.
func (b *EventBus) Subscribe(listener Listener) (unsubscribe func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.listeners[id] = listener
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.listeners, id)
		b.mu.Unlock()
	}
}

// Emit delivers event to every currently subscribed Listener. A
// snapshot of the subscriber set is taken before delivery so a
// listener that unsubscribes during Emit does not mutate the set
// being walked, and a panicking listener is isolated so it cannot
// prevent the others from receiving the event.
func (b *EventBus) Emit(event Event) {
	b.mu.Lock()
	snapshot := make([]Listener, 0, len(b.listeners))
	for _, listener := range b.listeners {
		snapshot = append(snapshot, listener)
	}
	b.mu.Unlock()

	for _, listener := range snapshot {
		deliver(listener, event)
	}
}

func deliver(listener Listener, event Event) {
	defer func() { _ = recover() }()
	listener(event)
}
