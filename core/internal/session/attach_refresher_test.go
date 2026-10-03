package session_test

import (
	"context"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/session"
)

const injectedTreeRevision = 4242

type countingRefresher struct{ calls atomic.Int64 }

func (r *countingRefresher) FreshTree(context.Context) (session.Snapshot, error) {
	r.calls.Add(1)
	return session.Snapshot{TreeRevision: injectedTreeRevision}, nil
}

func TestAttachTreeFresh_UsesTheRefresherTheHostInjected(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	refresher := &countingRefresher{}
	s, err := session.Open(context.Background(), baseDeps(t, &recordingFactory{events: &eventLog{}}, m),
		session.Request{Descriptor: androidDesc("A"), SocketRefresher: refresher})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	c := dialHolder(t, socketPathFor(m))

	var fresh attach.FreshResult
	if err := c.Call(context.Background(), attach.TypeTreeFresh, nil, &fresh); err != nil {
		t.Fatalf("tree.fresh while the session is running: %v, want the injected refresher to answer", err)
	}

	if refresher.calls.Load() != 1 || fresh.Tree.Revision != injectedTreeRevision {
		t.Fatalf("refresher calls = %d, revision = %d, want one call and revision %d", refresher.calls.Load(), fresh.Tree.Revision, injectedTreeRevision)
	}
}
