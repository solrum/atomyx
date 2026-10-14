//go:build e2e

package androiddriver_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// recordingRunner runs every command through the real runner and
// records the arguments of each adb forward command it saw.
type recordingRunner struct {
	inner process.Runner

	mu       sync.Mutex
	creates  []string
	removals []string
}

func (r *recordingRunner) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	result, err := r.inner.Run(ctx, name, args...)
	if name == "adb" {
		r.recordForward(args, result)
	}
	return result, err
}

// recordForward files a create under the host endpoint adb reported or
// was given, and a removal under the endpoint it was told to remove.
// Listing commands and commands for other subjects are ignored.
func (r *recordingRunner) recordForward(args []string, result process.Result) {
	for i, arg := range args {
		if arg != "forward" || i+1 >= len(args) {
			continue
		}
		rest := args[i+1:]
		r.mu.Lock()
		defer r.mu.Unlock()
		switch {
		case rest[0] == "--list":
		case rest[0] == "--remove" && len(rest) >= 2:
			r.removals = append(r.removals, rest[1])
		case rest[0] == "tcp:0":
			r.creates = append(r.creates, "tcp:"+strings.TrimSpace(result.Stdout))
		default:
			r.creates = append(r.creates, rest[0])
		}
		return
	}
}

func (r *recordingRunner) forwards() (creates, removals []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.creates...), append([]string(nil), r.removals...)
}

// TestAndroidDriver_E2E_Teardown_DisconnectRemovesEveryForwardItCreated
// connects a real driver through a recording command runner and checks
// that Disconnect removes each adb forward the driver created and that
// the host's forward table ends as it began. The Android driver starts
// no background process, so the forward is the only resource it
// owns. A forward that already existed for the serial is reused and
// never owned, so the case then records no create and no removal.
func TestAndroidDriver_E2E_Teardown_DisconnectRemovesEveryForwardItCreated(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformAndroid)
	serial := os.Getenv("ATOMYX_ANDROID_SERIAL")
	if serial == "" {
		t.Skip("skipping: set ATOMYX_ANDROID_SERIAL to the target device's adb serial")
	}

	runner := &recordingRunner{inner: process.NewExec()}
	ctx := context.Background()
	before := forwardTable(t, runner.inner)
	d := androiddriver.New(nil, runner, androiddriver.Options{
		Serial:           serial,
		AllocateHostPort: true,
		DevicePort:       androidE2EControlPort,
		NewTransport: func(hostPort int) androiddriver.Transport {
			return transport.NewHTTPClient("127.0.0.1", hostPort)
		},
	})
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	creates, removals := runner.forwards()
	if len(creates) == 0 {
		t.Logf("a forward for %s already existed and was reused, so Disconnect owed no removal", serial)
	}
	for _, created := range creates {
		if !contains(removals, created) {
			t.Errorf("forward %s was created and never removed: creates %v, removals %v", created, creates, removals)
		}
	}
	if after := forwardTable(t, runner.inner); after != before {
		t.Errorf("forward table changed across Connect and Disconnect:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func forwardTable(t *testing.T, runner process.Runner) string {
	t.Helper()
	listing, err := runner.Run(context.Background(), "adb", "forward", "--list")
	if err != nil {
		t.Fatalf("adb forward --list: %v", err)
	}
	return listing.Stdout
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
