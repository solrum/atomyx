package session_test

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/session"
)

const debugViewerWait = 5 * time.Second

func TestDebugViewers_TheHostIsToldOnlyWhenTheLastDebuggingViewerLeaves(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	gone := make(chan struct{}, 4)
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{
		Descriptor: androidDesc("A"), Debugger: &scriptedDebugger{},
		OnLastDebugViewerLeft: func() { gone <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	path := socketPathFor(m)

	watcher := dialHolder(t, path)
	if err := watcher.Call(context.Background(), attach.TypeStatus, nil, nil); err != nil {
		t.Fatal(err)
	}
	first, second := dialHolder(t, path), dialHolder(t, path)
	for _, c := range []*attach.Client{first, second} {
		if err := c.Call(context.Background(), attach.TypeDebugPause, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.DebugViewers(); got != 2 {
		t.Fatalf("DebugViewers() = %d, want the two connections that sent a debug request", got)
	}

	_ = first.Close()
	select {
	case <-gone:
		t.Fatal("the host was told while a debugging viewer was still connected")
	case <-time.After(100 * time.Millisecond):
	}
	_ = second.Close()
	select {
	case <-gone:
	case <-time.After(debugViewerWait):
		t.Fatal("the host was not told after the last debugging viewer left")
	}
	if got := s.DebugViewers(); got != 0 {
		t.Fatalf("DebugViewers() = %d after every viewer left", got)
	}
}
