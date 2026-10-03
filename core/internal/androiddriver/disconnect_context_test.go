package androiddriver_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
)

const disconnectGuard = 5 * time.Second

// blockingRemoveRunner answers every adb call from its Fake except a
// forward removal, which blocks until the call's context ends, like an adb
// server that does not answer.
type blockingRemoveRunner struct {
	*process.Fake
	removing chan struct{}
}

func (r *blockingRemoveRunner) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	if slices.Contains(args, "--remove") {
		close(r.removing)
		<-ctx.Done()
		return process.Result{}, ctx.Err()
	}
	return r.Fake.Run(ctx, name, args...)
}

func connectedOwnerOfAForward(t *testing.T, runner process.Runner) driver.ContextDisconnecter {
	t.Helper()
	d := allocatingDriver("S1", runner, newTransportRecorder())
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	return d
}

func newForwardingFake() *process.Fake {
	fake := process.NewFake()
	fake.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41001\n"}, nil)
	return fake
}

func TestAndroidDriver_DisconnectContext_AForwardRemovalEndsWithItsContext(t *testing.T) {
	runner := &blockingRemoveRunner{Fake: newForwardingFake(), removing: make(chan struct{})}
	d := connectedOwnerOfAForward(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.DisconnectContext(ctx) }()
	<-runner.removing

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DisconnectContext() error = %v", err)
		}
	case <-time.After(disconnectGuard):
		t.Fatal("the forward removal kept running after its context ended")
	}
}

func TestAndroidDriver_DisconnectContext_AContextThatIsDoneSkipsTheRemoval(t *testing.T) {
	fake := newForwardingFake()
	d := connectedOwnerOfAForward(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.DisconnectContext(ctx); err != nil {
		t.Fatalf("DisconnectContext() error = %v", err)
	}

	if got := countInvocations(fake, "-s", "S1", "forward", "--remove", "tcp:41001"); got != 0 {
		t.Fatalf("forward removals = %d, want none once the context is done", got)
	}
}
