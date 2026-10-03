package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

// yieldingHolder gives its session up when asked, and records who asked.
type yieldingHolder struct {
	sess     *session.Session
	borrower atomic.Value
}

func (h *yieldingHolder) Yield(_ context.Context, borrower string) (func(), error) {
	h.borrower.Store(borrower)
	return func() { _ = h.sess.Close() }, nil //nolint:contextcheck // the close runs after the reply, on its own goroutine
}

func holdYieldableDevice(t *testing.T, manager *lease.Manager) *yieldingHolder {
	t.Helper()
	holder := &yieldingHolder{}
	sess, err := session.Open(context.Background(), session.Deps{
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Leases:  manager,
		Sockets: manager,
		Clock:   clock.SystemClock{},
		Command: "atomyx inspect",
		Role:    "cli",
	}, session.Request{
		Descriptor: device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"},
		Yielder:    holder,
	})
	if err != nil {
		t.Fatalf("the holder could not take the device: %v", err)
	}
	holder.sess = sess
	t.Cleanup(func() { _ = sess.Close() })
	return holder
}

func TestRunBorrowsTheDeviceFromAnIdleHolderThatCanYield(t *testing.T) {
	manager := newShortLeaseManager(t)
	holder := holdYieldableDevice(t, manager)
	scriptPath := filepath.Join(t.TempDir(), "flow.yaml")
	if err := os.WriteFile(scriptPath, []byte("steps:\n  - sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	flags := map[string]FlagValue{"--file": scriptPath, "--platform": "android", "--device": "emulator-5554"}

	code := RunScript(context.Background(), leaseDeps(t, manager, &stdout, &stderr), flags)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 after borrowing the device; stderr: %s", code, stderr.String())
	}
	if got := holder.borrower.Load(); got != "atomyx run flow.yaml" {
		t.Fatalf("the holder was told the borrower is %v, want %q", got, "atomyx run flow.yaml")
	}
}
