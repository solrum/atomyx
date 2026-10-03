package device

import (
	"context"
	"fmt"

	"dev.atomyx/internal/process"
)

type lenderKey struct{}

// WithLender returns a context that names lender as the process that lent
// the device to whoever builds a driver with it. DriverFactory.Build
// passes it to the runner provisioner, so a runner the builder adopts
// records the lender and is handed back to it at the builder's exit. A
// zero lender returns ctx unchanged.
func WithLender(ctx context.Context, lender RunnerOwner) context.Context {
	if lender.Pid == 0 {
		return ctx
	}
	return context.WithValue(ctx, lenderKey{}, lender)
}

// LenderFrom returns the lender WithLender attached to ctx, or the zero
// value when there is none.
func LenderFrom(ctx context.Context) RunnerOwner {
	lender, _ := ctx.Value(lenderKey{}).(RunnerOwner)
	return lender
}

// OwnerResolver identifies live processes as runner owners.
type OwnerResolver struct {
	inspector process.Inspector
}

// NewOwnerResolver returns an OwnerResolver that reads process identity
// through inspector.
func NewOwnerResolver(inspector process.Inspector) OwnerResolver {
	return OwnerResolver{inspector: inspector}
}

// ResolveOwner returns the identity of the process with pid, which pairs
// the pid with its start time so a recycled pid is never mistaken for it.
// It fails when no such process exists.
func (r OwnerResolver) ResolveOwner(pid int) (RunnerOwner, error) {
	info, err := r.inspector.Inspect(pid)
	if err != nil {
		return RunnerOwner{}, fmt.Errorf("identify process %d: %w", pid, err)
	}
	return RunnerOwner{Pid: pid, StartTime: info.StartTime}, nil
}
