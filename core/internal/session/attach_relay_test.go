package session

import (
	"context"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/wire"
)

// hubOnlyHandlers serves the attach handlers from a session that holds
// nothing but a hub, which is all the notice relay reads.
func hubOnlyHandlers() (*attachHandlers, *Hub) {
	hub := NewHub()
	return &attachHandlers{s: &Session{hub: hub}}, hub
}

func drainNotices(notices <-chan attach.Notice) []attach.Notice {
	var out []attach.Notice
	for n := range notices {
		out = append(out, n)
	}
	return out
}

func TestRelay_ASubscriberThatFallsBehindGetsAClosedNoticeLast(t *testing.T) {
	h, hub := hubOnlyHandlers()
	notices, err := h.Subscribe(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 * subscriberBuffer {
		hub.ObserveTree(wire.TreeNodeWire{}, 1, true)
	}

	got := drainNotices(notices)

	if len(got) == 0 || got[len(got)-1].Type != attach.NoticeClosed {
		t.Fatalf("notices = %+v, want the stream to end with a closed notice", got)
	}
}

func TestRelay_AHubThatClosesEndsTheStreamWithAClosedNotice(t *testing.T) {
	h, hub := hubOnlyHandlers()
	notices, err := h.Subscribe(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}

	hub.Close()

	got := drainNotices(notices)
	if len(got) != 1 || got[0].Type != attach.NoticeClosed {
		t.Fatalf("notices = %+v, want one closed notice", got)
	}
}

func TestRelay_ACanceledSubscriptionEndsWithoutAClosedNotice(t *testing.T) {
	h, hub := hubOnlyHandlers()
	ctx, cancel := context.WithCancel(context.Background())
	notices, err := h.Subscribe(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	got := drainNotices(notices)

	if len(got) != 0 {
		t.Fatalf("notices = %+v, want none after the viewer canceled", got)
	}
	hub.ObserveTree(wire.TreeNodeWire{}, 1, true)
}
