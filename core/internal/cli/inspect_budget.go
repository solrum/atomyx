package cli

import (
	"context"
	"errors"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/session"
)

// inspectAttachBudgetMs is the total time one open of the inspector may
// spend waiting for holders across all its attempts. A single attach
// already waits up to this long for a holder that is still starting, so
// repeating it per attempt would multiply the wait.
const inspectAttachBudgetMs = session.AttachStartingBudgetMs

// attachBudget measures the time the attach attempts of one open have
// used, on the clock that times their waits.
type attachBudget struct {
	clk     clock.Clock
	startMs int64
}

func newAttachBudget(clk clock.Clock) attachBudget {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return attachBudget{clk: clk, startMs: clk.NowMs()}
}

// remainingMs is the time left for further attach attempts.
func (b attachBudget) remainingMs() int64 {
	return inspectAttachBudgetMs - (b.clk.NowMs() - b.startMs)
}

// errAttachBudget is the cancellation cause of an attempt cut off because
// the shared budget ran out.
var errAttachBudget = errors.New("the inspector's wait for the device holder is over")

// bound returns a context derived from ctx that ends with errAttachBudget
// as its cause when the remaining budget elapses. The returned stop
// releases the timer and must be called.
func (b attachBudget) bound(ctx context.Context) (context.Context, func()) {
	bounded, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if b.clk.Sleep(bounded, b.remainingMs()) == nil {
			cancel(errAttachBudget)
		}
	}()
	return bounded, func() {
		cancel(context.Canceled)
		<-done
	}
}

// cutOff reports whether the attempt context ended because the budget
// ran out, not because the caller's own context ended.
func cutOff(parent, attempt context.Context) bool {
	return parent.Err() == nil && errors.Is(context.Cause(attempt), errAttachBudget)
}
