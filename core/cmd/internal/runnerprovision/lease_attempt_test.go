package runnerprovision

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
)

type fakeTaker struct {
	err  error
	key  lease.Key
	held lease.Holder
}

func (f *fakeTaker) TryAcquire(_ context.Context, key lease.Key, holder lease.Holder) (*lease.Lease, error) {
	f.key, f.held = key, holder
	return nil, f.err
}

func TestLeaseAttempt_TakesTheIOSDeviceLeaseAsTheCleanupHolder(t *testing.T) {
	taker := &fakeTaker{err: errors.New("held")}

	_, err := leaseAttempt(taker, 4242, "atomyx")("UDID-1")

	if err == nil {
		t.Fatal("a held lease was reported as taken")
	}
	if want := lease.KeyFor(string(device.PlatformIOS), "UDID-1"); taker.key != want {
		t.Errorf("key = %q, want the iOS lease key %q", taker.key, want)
	}
	if taker.held.PID != 4242 || taker.held.Role != cleanupHolderRole {
		t.Errorf("holder = %+v, want pid 4242 and role %q", taker.held, cleanupHolderRole)
	}
}
