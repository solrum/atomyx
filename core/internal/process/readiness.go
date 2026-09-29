package process

import (
	"context"

	"dev.atomyx/internal/clock"
)

// ReadinessOutcome names which race arm resolved an AwaitReady call, so
// a caller can build its own domain-specific error for the arm that
// actually won without AwaitReady guessing at that error's shape.
type ReadinessOutcome int

const (
	// ReadinessReady means probe reported the process ready, or probe
	// itself returned a non-nil error: AwaitReady does not classify a
	// probe error further, since a probe worth writing already knows
	// what its own error means, so the caller distinguishes the two by
	// checking whether the returned error is nil.
	ReadinessReady ReadinessOutcome = iota
	// ReadinessExited means the exited channel produced a value before
	// probe reported ready.
	ReadinessExited
	// ReadinessTimeout means timeoutMs elapsed, per clk, before probe
	// reported ready or exited produced a value.
	ReadinessTimeout
	// ReadinessCtxDone means ctx was done before any of the above.
	ReadinessCtxDone
)

// AwaitReady races a readiness probe against a spawned process exiting
// early, a deadline enforced by clk, and ctx being canceled. This is the shape
// every "spawn a process, then wait for it to become reachable" launcher
// in this codebase needs. It owns only the race: how probe decides
// readiness (an immediate check, or a call that itself blocks until data
// or an error is available) and how the caller turns each outcome into a
// domain-specific error stay entirely up to the caller.
//
// probe is called once per iteration. A non-nil probeErr ends the race
// immediately, reported as ReadinessReady with that error returned
// verbatim: it is not classified further, since only the caller knows
// what its own probe error means. A probe that itself blocks until it
// has an answer (a blocking stdout read, for example) turns AwaitReady
// into a single-shot wait rather than a poll loop, because that one call
// already decides the outcome.
//
// exited may be nil when the caller has no exit signal independent of
// probe itself (a nil channel never fires in a select, so the race then
// reduces to probe versus ctx versus timeout). Its element is already
// the caller's own fully-built error for that exit, since only the
// caller knows how to turn a process outcome into a domain-specific
// error.
//
// pollIntervalMs and timeoutMs of zero or less disable the
// corresponding behavior: a zero pollIntervalMs skips the sleep between
// iterations (appropriate when probe already blocks until it has
// something to report), and a zero timeoutMs never times out.
func AwaitReady(
	ctx context.Context,
	clk clock.Clock,
	probe func() (ready bool, probeErr error),
	exited <-chan error,
	pollIntervalMs int64,
	timeoutMs int64,
) (outcome ReadinessOutcome, err error) {
	var deadlineMs int64
	if timeoutMs > 0 {
		deadlineMs = clk.NowMs() + timeoutMs
	}
	for {
		ready, probeErr := probe()
		if probeErr != nil {
			return ReadinessReady, probeErr
		}
		if ready {
			return ReadinessReady, nil
		}

		select {
		case exitErr := <-exited:
			return ReadinessExited, exitErr
		case <-ctx.Done():
			return ReadinessCtxDone, ctx.Err()
		default:
		}

		if timeoutMs > 0 && clk.NowMs() >= deadlineMs {
			return ReadinessTimeout, nil
		}
		if pollIntervalMs > 0 {
			if err := clk.Sleep(ctx, pollIntervalMs); err != nil {
				return ReadinessCtxDone, err
			}
		}
	}
}
