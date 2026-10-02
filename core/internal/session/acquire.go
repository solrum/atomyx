package session

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
)

// errAcquireBudget is the cancellation cause of an Open whose
// acquisition budget elapsed, so it can be told apart from a canceled
// caller.
var errAcquireBudget = errors.New("device acquisition timeout elapsed")

// Phase is a stage of Acquire a host can report to its user.
type Phase string

const (
	// PhaseBuilding starts the lease-free preparation, such as the
	// runner build.
	PhaseBuilding Phase = "building"
	// PhaseLaunching starts the lease and the launch of the driver.
	PhaseLaunching Phase = "launching"
	// PhaseConnected reports that the device is open.
	PhaseConnected Phase = "connected"
)

// Acquisition tunes Acquire.
type Acquisition struct {
	// BudgetMs bounds Open. Non-positive means
	// DefaultDeviceAcquireTimeoutMs.
	BudgetMs int64
	// Clock times the acquisition budget. Defaults to deps.Clock, then
	// to the system clock.
	Clock clock.Clock
	// Progress, when set, is told each Phase as it starts.
	Progress func(Phase)
}

// Acquire opens the device named by req the way every host should:
// Prepare under the runner build timeout, then Open under the
// acquisition budget, which never includes the build. A build that
// outlasts its timeout fails with RUNNER_BUILD_TIMEOUT. An Open that
// outlasts the budget fails with TOOL_TIMEOUT and a remedy, never a
// bare context error. A canceled ctx returns its own error.
func Acquire(ctx context.Context, deps Deps, req Request, acq Acquisition) (*Session, error) {
	report := func(p Phase) {
		if acq.Progress != nil {
			acq.Progress(p)
		}
	}
	report(PhaseBuilding)
	if err := Prepare(ctx, deps, req.Descriptor); err != nil {
		return nil, err
	}
	return OpenWithinBudget(ctx, deps, req, acq)
}

// OpenWithinBudget runs the Open half of Acquire for a host that
// already ran Prepare itself, for example to keep the long build outside
// a lock the open must hold. It reports PhaseLaunching and
// PhaseConnected, and maps an elapsed budget like Acquire does.
func OpenWithinBudget(ctx context.Context, deps Deps, req Request, acq Acquisition) (*Session, error) {
	report := func(p Phase) {
		if acq.Progress != nil {
			acq.Progress(p)
		}
	}
	report(PhaseLaunching)
	budgetMs := acq.BudgetMs
	if budgetMs <= 0 {
		budgetMs = DefaultDeviceAcquireTimeoutMs
	}
	clk := acq.Clock
	if clk == nil {
		clk = deps.withDefaults().Clock
	}
	acquireCtx, stop := withClockTimeout(ctx, clk, budgetMs, errAcquireBudget)
	defer stop()
	sess, err := Open(acquireCtx, deps, req)
	if err == nil {
		report(PhaseConnected)
		return sess, nil
	}
	if ctx.Err() == nil && errors.Is(context.Cause(acquireCtx), errAcquireBudget) {
		return nil, diag.New(diag.CodeToolTimeout,
			fmt.Sprintf("acquiring the device did not finish within %d seconds", budgetMs/1000),
			"Check that the device is connected, unlocked and trusted, then try again.", err)
	}
	return nil, err
}
