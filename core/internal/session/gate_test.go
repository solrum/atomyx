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
	exclusive, err := g.Exclusive(context.Background())
	if err != nil {
		t.Fatalf("Exclusive after every call left and a repeated release: %v", err)
	}
	exclusive()
}

func TestActivityGate_ExclusiveWaitsForCallsInsideAndHoldsBackLaterCalls(t *testing.T) {
	g := session.NewActivityGate()
	inside, err := g.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	exclusiveReady := make(chan func(), 1)
	go func() {
		release, _ := g.Exclusive(context.Background())
		exclusiveReady <- release
	}()
	awaitWaiting(g, 1)
	select {
	case <-exclusiveReady:
		t.Fatal("Exclusive was admitted while a call was inside")
	default:
	}

	lateReady := make(chan func(), 1)
	go func() {
		release, _ := g.Enter(context.Background())
		lateReady <- release
	}()
	awaitWaiting(g, 2)
	select {
	case <-lateReady:
		t.Fatal("a call entered ahead of a waiting Exclusive")
	default:
	}

	inside()
	releaseExclusive := <-exclusiveReady
	select {
	case <-lateReady:
		t.Fatal("a call entered while Exclusive held the gate")
	default:
	}
	releaseExclusive()
	(<-lateReady)()
}

func TestActivityGate_WaitersHonorTheirContext(t *testing.T) {
	g := session.NewActivityGate()
	inside, err := g.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := g.Exclusive(ctx)
		done <- err
	}()
	awaitWaiting(g, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Exclusive error = %v, want context.Canceled", err)
	}

	// The canceled waiter no longer holds back later calls.
	another, err := g.Enter(context.Background())
	if err != nil {
		t.Fatalf("Enter after the waiter gave up: %v", err)
	}
	another()
	inside()
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
	if _, err := g.Exclusive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exclusive error = %v, want context.Canceled", err)
	}
}
