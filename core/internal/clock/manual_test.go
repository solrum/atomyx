package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManualClockSleepWakesOnlyOnceTheDeadlinePasses(t *testing.T) {
	c := NewManualClock(100)
	woke := make(chan error, 1)
	go func() { woke <- c.Sleep(context.Background(), 50) }()
	c.AwaitSleepers(1)

	c.Advance(49)
	select {
	case <-woke:
		t.Fatal("Sleep returned before its deadline")
	default:
	}
	c.Advance(1)
	select {
	case err := <-woke:
		if err != nil {
			t.Fatalf("Sleep error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Sleep did not return at its deadline")
	}
	if got := c.NowMs(); got != 150 {
		t.Fatalf("NowMs = %d, want 150", got)
	}
}

func TestManualClockSleepReturnsWhenContextIsCanceled(t *testing.T) {
	c := NewManualClock(0)
	ctx, cancel := context.WithCancel(context.Background())
	woke := make(chan error, 1)
	go func() { woke <- c.Sleep(ctx, 1000) }()
	c.AwaitSleepers(1)
	cancel()
	if err := <-woke; err == nil {
		t.Fatal("Sleep returned nil after its context was canceled")
	}
	c.AwaitIdle()
}

func TestManualClock_ACanceledSleeperIsNotCountedAndIsNotWoken(t *testing.T) {
	c := NewManualClock(0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Sleep(ctx, 100) }()
	c.AwaitSleepers(1)

	cancel()
	c.Advance(100)

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep() = %v, want the cancellation, not a normal wake", err)
	}
	c.AwaitIdle()
}
