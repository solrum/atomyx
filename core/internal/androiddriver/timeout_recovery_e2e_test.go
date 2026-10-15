//go:build e2e

package androiddriver_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

const (
	// e2eClientBound is the client's own fallback bound. It applies to
	// every call through the proxy, so it is long enough for an
	// unheld hierarchy dump and shorter than the hold.
	e2eClientBound = 3 * time.Second
	// e2eHoldDuration is how long the proxy holds the chosen request.
	e2eHoldDuration = 10 * time.Second
)

// TestAndroidDriver_E2E_TimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds
// puts a delay proxy between the driver and the real agent, holds the
// first hierarchy request past the client's own bound, and checks the
// call fails with TOOL_TIMEOUT and that the next calls succeed against
// the same agent. The call carries no context deadline, so the
// classification comes from the transport's own fallback bound.
func TestAndroidDriver_E2E_TimeoutRecovery_HeldReplyIsToolTimeoutAndNextCallSucceeds(t *testing.T) {
	drivertest.RequireDevice(t, drivertest.E2EPlatformAndroid)
	serial := os.Getenv("ATOMYX_ANDROID_SERIAL")
	if serial == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset, "set ATOMYX_ANDROID_SERIAL to the target device's adb serial")
	}

	proxyPort := drivertest.NewHTTPDelayProxy(t, fmt.Sprintf("127.0.0.1:%d", androidE2EControlPort), "/tree", e2eHoldDuration)
	client := transport.NewHTTPClient("127.0.0.1", proxyPort, transport.WithHTTPClient(&http.Client{Timeout: e2eClientBound}))
	d := androiddriver.New(client, process.NewExec(), androiddriver.Options{
		Serial:     serial,
		HostPort:   androidE2EControlPort,
		DevicePort: androidE2EControlPort,
	})
	ctx := context.Background()
	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = d.Disconnect() })

	_, err := d.Hierarchy(ctx)
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("held Hierarchy error = %v (code %q), want code %q", err, code, diag.CodeToolTimeout)
	}
	if err := d.Ping(ctx); err != nil {
		t.Fatalf("Ping after the timeout: %v", err)
	}
	if _, err := d.Hierarchy(ctx); err != nil {
		t.Fatalf("Hierarchy after the timeout: %v", err)
	}
}
