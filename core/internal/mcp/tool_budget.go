package mcp

import (
	"context"
	"sync"
	"time"

	"dev.atomyx/internal/clock"
)

// budgetContextKey is the context key under which a call's budgetContext
// finds itself, so code far below the middleware can pause the budget
// without a parameter threading it.
type budgetContextKey struct{}

// budgetContext is the context a tool call runs under. It ends with
// context.DeadlineExceeded when its time budget elapses, like a context
// with a deadline, but the budget can be paused: a call that must
// acquire a device does that outside the budget, and the budget starts
// over, in full, once acquisition has finished.
type budgetContext struct {
	context.Context
	clk      clock.Clock
	budgetMs int64

	mu          sync.Mutex
	done        chan struct{}
	err         error
	paused      bool
	armedAtMs   int64
	cancelTimer context.CancelFunc
	stopParent  func() bool
	timers      sync.WaitGroup
}

// newBudgetContext returns a context derived from parent that expires
// budgetMs milliseconds of clk time after creation. The returned stop
// function releases its timer and must be called when the call ends.
func newBudgetContext(parent context.Context, clk clock.Clock, budgetMs int64) (context.Context, func()) {
	b := &budgetContext{Context: parent, clk: clk, budgetMs: budgetMs, done: make(chan struct{})}
	b.mu.Lock()
	b.armLocked()
	b.mu.Unlock()
	b.stopParent = context.AfterFunc(parent, func() { b.finish(parent.Err()) })
	return b, func() {
		b.stopParent()
		b.finish(context.Canceled)
		b.timers.Wait()
	}
}

// armLocked starts the timer for a full budget.
func (b *budgetContext) armLocked() {
	timerCtx, cancel := context.WithCancel(b.Context)
	b.cancelTimer = cancel
	b.armedAtMs = b.clk.NowMs()
	b.timers.Add(1)
	go func() {
		defer b.timers.Done()
		if b.clk.Sleep(timerCtx, b.budgetMs) == nil {
			b.expire(timerCtx)
		}
	}()
}

// expire ends the call with DeadlineExceeded, unless the timer that
// fired was replaced or stopped by a pause in the meantime. A clock can
// wake a sleeper at the same moment its context is canceled, so the
// check happens under the lock that pause also takes.
func (b *budgetContext) expire(fired context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if fired.Err() != nil || b.paused {
		return
	}
	b.finishLocked(context.DeadlineExceeded)
}

func (b *budgetContext) finish(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finishLocked(err)
}

func (b *budgetContext) finishLocked(err error) {
	if b.err != nil {
		return
	}
	b.err = err
	b.cancelTimer()
	close(b.done)
}

// Done implements context.Context.
func (b *budgetContext) Done() <-chan struct{} { return b.done }

// Err implements context.Context.
func (b *budgetContext) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// Deadline implements context.Context. It reports the remaining budget
// as a wall-clock deadline while the budget runs, and no deadline while
// it is paused, because a paused budget has no moment at which it ends.
func (b *budgetContext) Deadline() (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.paused || b.err != nil {
		return time.Time{}, false
	}
	remaining := b.budgetMs - (b.clk.NowMs() - b.armedAtMs)
	return time.Now().Add(time.Duration(remaining) * time.Millisecond), true
}

// Value implements context.Context.
func (b *budgetContext) Value(key any) any {
	if _, ok := key.(budgetContextKey); ok {
		return b
	}
	return b.Context.Value(key)
}

// pause stops the budget and returns the function that restarts it with
// a full budget. A budget that already ended, or is already paused,
// returns a no-op.
func (b *budgetContext) pause() (resume func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil || b.paused {
		return func() {}
	}
	b.paused = true
	b.cancelTimer()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.err != nil || !b.paused {
			return
		}
		b.paused = false
		b.armLocked()
	}
}

// pauseToolBudget pauses the time budget of the tool call ctx belongs
// to, and returns the function that restarts it. A ctx outside any
// tool call returns a no-op, so code that is also reached from a CLI
// never needs to know whether a budget exists.
func pauseToolBudget(ctx context.Context) (resume func()) {
	b, ok := ctx.Value(budgetContextKey{}).(*budgetContext)
	if !ok {
		return func() {}
	}
	return b.pause() //nolint:contextcheck // the restarted budget timer is owned by the call context it was created with
}
