package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

func TestLenderTravelsInTheContext(t *testing.T) {
	lender := RunnerOwner{Pid: 4242, StartTime: time.Unix(1_700_000_000, 0).UTC()}
	if got := LenderFrom(WithLender(context.Background(), lender)); got != lender {
		t.Fatalf("LenderFrom() = %+v, want %+v", got, lender)
	}
	if got := LenderFrom(WithLender(context.Background(), RunnerOwner{})); got.Pid != 0 {
		t.Fatalf("a zero lender produced %+v, want none", got)
	}
}

func TestOwnerResolverPairsThePidWithItsStartTime(t *testing.T) {
	started := time.Unix(1_700_000_000, 0).UTC()
	inspector := process.NewFakeInspector(started.Add(-time.Hour))
	inspector.Set(process.ProcessInfo{PID: 77, StartTime: started})
	resolver := NewOwnerResolver(inspector)

	owner, err := resolver.ResolveOwner(77)
	if err != nil || owner.Pid != 77 || !owner.StartTime.Equal(started) {
		t.Fatalf("ResolveOwner(77) = %+v, %v", owner, err)
	}
	if _, err := resolver.ResolveOwner(78); !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("ResolveOwner(78) error = %v, want not found", err)
	}
}
