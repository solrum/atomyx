//go:build e2e

package androiddriver_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// TestAndroidDriver_E2E_AllocateHostPort_TwoSerialsDistinctPortsAndReuse
// connects real drivers to two attached devices through
// AllocateHostPort and checks the adb forward table. It needs the
// agent running on both serials named by ATOMYX_ANDROID_SERIAL_A and
// ATOMYX_ANDROID_SERIAL_B.
func TestAndroidDriver_E2E_AllocateHostPort_TwoSerialsDistinctPortsAndReuse(t *testing.T) {
	serialA := os.Getenv("ATOMYX_ANDROID_SERIAL_A")
	serialB := os.Getenv("ATOMYX_ANDROID_SERIAL_B")
	if serialA == "" || serialB == "" {
		t.Skip("skipping: set ATOMYX_ANDROID_SERIAL_A and ATOMYX_ANDROID_SERIAL_B to two attached devices")
	}
	runner := process.NewExec()
	newDriver := func(serial string, ports *[]int) *androiddriver.AndroidDriver {
		return androiddriver.New(nil, runner, androiddriver.Options{
			Serial:           serial,
			AllocateHostPort: true,
			DevicePort:       8765,
			NewTransport: func(hostPort int) androiddriver.Transport {
				*ports = append(*ports, hostPort)
				return transport.NewHTTPClient("127.0.0.1", hostPort)
			},
		})
	}
	var portsA, portsB, portsA2 []int
	a, b := newDriver(serialA, &portsA), newDriver(serialB, &portsB)
	ctx := context.Background()
	if err := a.Connect(ctx); err != nil {
		t.Fatalf("connect %s: %v", serialA, err)
	}
	if err := b.Connect(ctx); err != nil {
		t.Fatalf("connect %s: %v", serialB, err)
	}
	t.Logf("%s host port %v, %s host port %v", serialA, portsA, serialB, portsB)
	if len(portsA) != 1 || len(portsB) != 1 || portsA[0] == portsB[0] {
		t.Fatalf("host ports A=%v B=%v, want two distinct ports", portsA, portsB)
	}

	a2 := newDriver(serialA, &portsA2)
	if err := a2.Connect(ctx); err != nil {
		t.Fatalf("second connect %s: %v", serialA, err)
	}
	if len(portsA2) != 1 || portsA2[0] != portsA[0] {
		t.Fatalf("second driver for %s used port %v, want reuse of %v", serialA, portsA2, portsA)
	}
	listing, err := runner.Run(ctx, "adb", "forward", "--list")
	if err != nil {
		t.Fatalf("adb forward --list: %v", err)
	}
	if n := strings.Count(listing.Stdout, serialA+" "); n != 1 {
		t.Fatalf("forwards for %s = %d, want exactly one:\n%s", serialA, n, listing.Stdout)
	}

	_ = a2.Disconnect()
	_ = b.Disconnect()
	_ = a.Disconnect()
}
