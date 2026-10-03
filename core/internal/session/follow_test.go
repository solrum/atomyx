package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

const followTestDeadline = time.Second

// budgetBlockingClock returns every short wait at once and holds a wait of
// the follow budget until its context ends, so the budget never expires
// during a test.
type budgetBlockingClock struct{}

func (budgetBlockingClock) NowMs() int64 { return 0 }

func (budgetBlockingClock) Sleep(ctx context.Context, ms int64) error {
	if ms >= session.FollowBudgetMs {
		<-ctx.Done()
		return ctx.Err()
	}
	return ctx.Err()
}

func TestFollow_AHolderThatEndsBeforeItCanBeWatchedEndsTheFollowAtOnce(t *testing.T) {
	gone := heldStatus(lease.Holder{PID: 7, Command: "atomyx run", Socket: t.TempDir() + "/gone.sock"})
	prober := &scriptedProber{script: []lease.Status{gone, gone, {}}}
	ctx, cancel := context.WithTimeout(context.Background(), followTestDeadline)
	defer cancel()

	_, err := session.Follow(ctx, session.AttachDeps{Leases: prober, Clock: budgetBlockingClock{}}, androidDesc("A"))

	if !errors.Is(err, session.ErrBorrowerEnded) {
		t.Fatalf("Follow() error = %v, want ErrBorrowerEnded", err)
	}
}

func TestFollow_ADeviceNobodyTookKeepsProbingUntilTheBudgetEnds(t *testing.T) {
	prober := &scriptedProber{script: []lease.Status{{}}}
	ctx, cancel := context.WithTimeout(context.Background(), followTestDeadline)
	defer cancel()

	_, err := session.Follow(ctx, session.AttachDeps{Leases: prober, Clock: budgetBlockingClock{}}, androidDesc("A"))

	if errors.Is(err, session.ErrBorrowerEnded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Follow() error = %v, want it to keep probing a device that was never held", err)
	}
}
