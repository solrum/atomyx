package cli

import (
	"context"
	"testing"
)

func TestAttachBudget_AnElapsedBudgetCutsTheAttemptOffAndACallerCancelDoesNot(t *testing.T) {
	clk := &skipClock{}
	budget := newAttachBudget(clk)

	attempt, stop := budget.bound(context.Background())
	<-attempt.Done()
	stop()
	if !cutOff(context.Background(), attempt) {
		t.Fatal("an attempt whose budget elapsed was not reported as cut off")
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	attempt, stop = budget.bound(parent)
	<-attempt.Done()
	stop()
	if cutOff(parent, attempt) {
		t.Fatal("an attempt the caller canceled was reported as cut off")
	}
}

func TestAttachBudget_RemainingShrinksWithTheClock(t *testing.T) {
	clk := &skipClock{}
	budget := newAttachBudget(clk)
	clk.now.Add(inspectAttachBudgetMs / 4)

	if got := budget.remainingMs(); got != inspectAttachBudgetMs*3/4 {
		t.Fatalf("remaining = %d, want three quarters of the budget", got)
	}
}
