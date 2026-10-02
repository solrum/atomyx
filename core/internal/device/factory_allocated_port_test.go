package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"dev.atomyx/internal/process"
)

func startHealthyAgent(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"accessibilityConnected":true}`))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return port
}

// TestDriverFactory_Build_Android_TwoSerialsReachTheirOwnAllocatedPorts
// proves the factory asks adb for a free host port per device and
// dials each driver at the port adb printed for its own serial.
func TestDriverFactory_Build_Android_TwoSerialsReachTheirOwnAllocatedPorts(t *testing.T) {
	portA := startHealthyAgent(t)
	portB := startHealthyAgent(t)
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "A", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: strconv.Itoa(portA) + "\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "B", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: strconv.Itoa(portB) + "\n"}, nil)
	f := NewDriverFactory(DriverFactoryDeps{Runner: runner, Background: process.NewFakeBackground()})

	for _, serial := range []string{"A", "B"} {
		built, err := f.Build(context.Background(), Descriptor{ID: serial, Platform: PlatformAndroid})
		if err != nil {
			t.Fatalf("Build(%s) error = %v", serial, err)
		}
		if !built.Driver.IsConnected() {
			t.Fatalf("Build(%s) returned a driver that is not connected", serial)
		}
	}
}
