package drivertest

import (
	"context"
	"time"

	"dev.atomyx/internal/driver"
)

const (
	// foregroundSettleTimeout bounds how long a corpus case waits for
	// CurrentForeground to reflect a launch or stop, because the
	// contract only promises the report catches up shortly after the
	// action returns.
	foregroundSettleTimeout = 5 * time.Second

	// foregroundSettlePoll is the gap between reads while waiting.
	foregroundSettlePoll = 100 * time.Millisecond
)

// awaitForeground reads CurrentForeground until bundleID is in front
// (wantFront) or no longer in front (not wantFront), and returns the
// last result so the caller's assertion reports a real mismatch after
// the bounded wait. An adapter without foreground support, or a read
// that fails, returns at once because waiting cannot change it.
func awaitForeground(ctx context.Context, d driver.Driver, bundleID string, wantFront bool) (driver.ForegroundInfo, error) {
	deadline := time.Now().Add(foregroundSettleTimeout)
	for {
		info, err := d.CurrentForeground(ctx)
		if err != nil || !d.Capabilities().CanQueryForeground {
			return info, err
		}
		inFront := info.BundleID != nil && *info.BundleID == bundleID
		if inFront == wantFront || !time.Now().Before(deadline) {
			return info, nil
		}
		select {
		case <-ctx.Done():
			return info, nil
		case <-time.After(foregroundSettlePoll):
		}
	}
}
