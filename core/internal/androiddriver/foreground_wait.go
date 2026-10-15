package androiddriver

import (
	"context"
	"fmt"
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
// and the action itself already succeeded. It returns ctx's error
// when ctx ends first. It does nothing when the agent cannot report
// the foreground.
func (d *AndroidDriver) awaitForeground(ctx context.Context, bundleID, expectation string, reached func(current string) bool) error {
	if !d.capabilities.CanQueryForeground {
		return nil
	}
	deadline := d.opts.Clock.NowMs() + foregroundSettleTimeoutMs
	for {
		info, err := d.CurrentForeground(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
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
			d.warn(fmt.Sprintf("the foreground did not report %s %s within %d ms", bundleID, expectation, foregroundSettleTimeoutMs))
			return nil
		}
		if err := d.opts.Clock.Sleep(ctx, foregroundSettlePollMs); err != nil {
			return err
		}
	}
}

func (d *AndroidDriver) warn(message string) {
	if d.opts.Warn != nil {
		d.opts.Warn(message)
	}
}
