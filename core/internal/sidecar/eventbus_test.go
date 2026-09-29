package sidecar

import (
	"sync"
	"testing"
)

func TestEventBus_Emit_DeliversToAllSubscribers(t *testing.T) {
	b := NewEventBus()
	var mu sync.Mutex
	var gotA, gotB []Event

	b.Subscribe(func(e Event) { mu.Lock(); gotA = append(gotA, e); mu.Unlock() })
	b.Subscribe(func(e Event) { mu.Lock(); gotB = append(gotB, e); mu.Unlock() })

	b.Emit(Event{Event: "x", Payload: 1})

	mu.Lock()
	defer mu.Unlock()
	if len(gotA) != 1 || len(gotB) != 1 {
		t.Fatalf("subscriber counts = %d, %d, want 1, 1", len(gotA), len(gotB))
	}
}

func TestEventBus_Unsubscribe_StopsDelivery(t *testing.T) {
	b := NewEventBus()
	count := 0
	unsubscribe := b.Subscribe(func(Event) { count++ })

	b.Emit(Event{Event: "x"})
	unsubscribe()
	b.Emit(Event{Event: "x"})

	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestEventBus_Emit_PanickingListener_DoesNotBlockOthers(t *testing.T) {
	b := NewEventBus()
	b.Subscribe(func(Event) { panic("boom") })
	delivered := false
	b.Subscribe(func(Event) { delivered = true })

	b.Emit(Event{Event: "x"})

	if !delivered {
		t.Fatal("the second listener did not receive the event after the first one panicked")
	}
}

func TestEventBus_Unsubscribe_DuringEmit_DoesNotMutateInFlightDelivery(t *testing.T) {
	b := NewEventBus()
	var unsubscribeSelf func()
	selfCalls := 0
	unsubscribeSelf = b.Subscribe(func(Event) {
		selfCalls++
		unsubscribeSelf()
	})
	otherCalls := 0
	b.Subscribe(func(Event) { otherCalls++ })

	b.Emit(Event{Event: "x"})
	b.Emit(Event{Event: "x"})

	if selfCalls != 1 {
		t.Fatalf("selfCalls = %d, want 1 (unsubscribed after first delivery)", selfCalls)
	}
	if otherCalls != 2 {
		t.Fatalf("otherCalls = %d, want 2", otherCalls)
	}
}
