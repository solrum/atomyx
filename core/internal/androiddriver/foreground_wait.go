package androiddriver

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// foregroundSettleTimeoutMs bounds how long LaunchApp and StopApp
	// wait for the reported foreground package to reflect the change,
	// because the agent's view trails the system by up to about a second.
	foregroundSettleTimeoutMs = 5000

	// foregroundSettlePollMs is the gap between foreground reads while
	// waiting, short enough to notice a change promptly without flooding
	// the agent.
	foregroundSettlePollMs = 100
)

// awaitForeground polls the reported foreground package until
// reached reports true for it, the settle budget runs out, or a
// foreground read fails. Only the package is compared, never the
// activity class, because the class stays a placeholder for seconds
// after a launch. A timeout or a read failure emits one warning and
// returns nil, since the app may have handed off to another package
// and the action itself already succeeded. The budget never extends
// past ctx's deadline, and a deadline that ends the wait counts as a
// timeout, because the action it follows already succeeded. An explicit
// cancellation returns ctx's error, because the caller abandoned the
// call. It does nothing when the agent cannot report the foreground.
func (d *AndroidDriver) awaitForeground(ctx context.Context, bundleID, expectation string, reached func(current string) bool) error {
	if !d.capabilities.CanQueryForeground {
		return nil
	}
	budgetMs := int64(foregroundSettleTimeoutMs)
	if limit, ok := ctx.Deadline(); ok {
		if remaining := time.Until(limit).Milliseconds(); remaining < budgetMs {
			budgetMs = max(remaining, 0)
		}
	}
	deadline := d.opts.Clock.NowMs() + budgetMs
	for {
		info, err := d.CurrentForeground(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return d.waitEnded(ctxErr, bundleID, expectation)
			}
			d.warn(fmt.Sprintf("stopped waiting for %s %s: the foreground query failed: %v", bundleID, expectation, err))
			return nil
		}
		current := ""
		if info.BundleID != nil {
			current = *info.BundleID
		}
		if reached(current) {
			return nil
		}
		if d.opts.Clock.NowMs() >= deadline {
			d.warn(fmt.Sprintf("the foreground did not report %s %s within %d ms", bundleID, expectation, budgetMs))
			return nil
		}
		if err := d.opts.Clock.Sleep(ctx, foregroundSettlePollMs); err != nil {
			return d.waitEnded(err, bundleID, expectation)
		}
	}
}

// waitEnded decides the result of a wait that ctx cut short: nil with a
// warning when the deadline ran out, ctx's own error otherwise.
func (d *AndroidDriver) waitEnded(err error, bundleID, expectation string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		d.warn(fmt.Sprintf("the deadline ended the wait for %s %s", bundleID, expectation))
		return nil
	}
	return err
}

func (d *AndroidDriver) warn(message string) {
	if d.opts.Warn != nil {
		d.opts.Warn(message)
	}
}
