package sessiontest

import (
	"context"
	"sync/atomic"

	"dev.atomyx/internal/attach"
)

// YieldSeam is a session.YieldRequester that counts the requests a host
// sends, remembers the borrower string of the last one, and then performs
// the real request.
type YieldSeam struct {
	calls    atomic.Int32
	borrower atomic.Value
}

// Requester returns the function to set as session.Deps.RequestYield.
func (y *YieldSeam) Requester() func(context.Context, attach.ClientConfig, string) (attach.YieldResult, error) {
	return func(ctx context.Context, cfg attach.ClientConfig, command string) (attach.YieldResult, error) {
		y.calls.Add(1)
		y.borrower.Store(command)
		return attach.RequestYield(ctx, cfg, command)
	}
}

// Calls returns how many yield requests were sent.
func (y *YieldSeam) Calls() int { return int(y.calls.Load()) }

// Borrower returns the borrower string of the last request, or empty when
// none was sent.
func (y *YieldSeam) Borrower() string {
	b, _ := y.borrower.Load().(string)
	return b
}
