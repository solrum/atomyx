package session_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"dev.atomyx/internal/session"
)

func awaitWaiting(g *session.ActivityGate, want int) {
	for g.Waiting() < want {
		runtime.Gosched()
	}
}

func TestActivityGate_ToolCallsShareTheGate(t *testing.T) {
	g := session.NewActivityGate()
	first, err := g.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first()
	second()
	second()
	exclusive, ok := g.TryExclusive()
	if !ok {
		t.Fatal("TryExclusive refused after every call left and a repeated release")
	}
	exclusive()
}

func TestActivityGate_TryExclusiveRefusesAtOnceWhileACallIsInsideOrACaptureHoldsTheGate(t *testing.T) {
	g := session.NewActivityGate()
	inside, err := g.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.TryExclusive(); ok {
		t.Fatal("TryExclusive was admitted while a call was inside")
	}
	if g.Waiting() != 0 {
		t.Fatalf("Waiting() = %d, want a refused TryExclusive not to wait", g.Waiting())
	}
	inside()

	release, ok := g.TryExclusive()
	if !ok {
		t.Fatal("TryExclusive refused on a free gate")
	}
	if _, ok := g.TryExclusive(); ok {
		t.Fatal("a second TryExclusive was admitted while one held the gate")
	}
	release()
	release()
	if again, ok := g.TryExclusive(); !ok {
		t.Fatal("TryExclusive refused after the capture released")
	} else {
		again()
	}
}

func TestActivityGate_ACallWaitsForACaptureToEnd(t *testing.T) {
	g := session.NewActivityGate()
	capture, ok := g.TryExclusive()
	if !ok {
		t.Fatal("TryExclusive refused on a free gate")
	}

	entered := make(chan func(), 1)
	go func() {
		release, _ := g.Enter(context.Background())
		entered <- release
	}()
	awaitWaiting(g, 1)
	select {
	case <-entered:
		t.Fatal("a call entered while a capture held the gate")
	default:
	}

	capture()
	(<-entered)()
}

func TestActivityGate_AWaitingCallHonorsItsContext(t *testing.T) {
	g := session.NewActivityGate()
	capture, _ := g.TryExclusive()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.Enter(ctx)
		done <- err
	}()
	awaitWaiting(g, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Enter error = %v, want context.Canceled", err)
	}
	capture()
	if g.Waiting() != 0 {
		t.Fatalf("Waiting() = %d, want none", g.Waiting())
	}
}

func TestActivityGate_AnEndedContextNeverEntersAFreeGate(t *testing.T) {
	g := session.NewActivityGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Enter(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enter error = %v, want context.Canceled", err)
	}
}
