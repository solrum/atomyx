//go:build e2e

package iosdriver_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

const (
	iosE2EDemoAppID    = "dev.atomyx.demo"
	iosE2EFocusRoute   = "/focus"
	iosE2EFocusInput   = "focus-input"
	iosE2ECommandLimit = 60 * time.Second
)

// iosE2EFixture builds drivers for the corpus and remembers the latest
// connection, because the fault hooks act on the instance the current
// sub-test is exercising.
type iosE2EFixture struct {
	udid string
	kind iosdriver.Kind

	mu    sync.Mutex
	conns []*faultConn
}

func (f *iosE2EFixture) newDriver() driver.Driver {
	conn := &faultConn{TCPClient: transport.NewTCPClient("127.0.0.1", iosE2EControlPort)}
	f.mu.Lock()
	f.conns = append(f.conns, conn)
	f.mu.Unlock()
	return iosdriver.New(conn, process.NewExec(), iosdriver.Options{
		Background: process.NewExecBackground(),
		UDID:       f.udid,
		Kind:       f.kind,
		Port:       iosE2EControlPort,
	})
}

func (f *iosE2EFixture) current() *faultConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[len(f.conns)-1]
}

// faultHooks wires the two fault hooks every iOS leg supports. Both
// act only on the client this test built for the driver, so no tunnel,
// runner, or port owned by anyone else is touched.
//
//   - FailConnect aims the next Connect at a closed local port.
//   - FailPing closes the client, so the next Ping finds no connection.
func (f *iosE2EFixture) faultHooks(hooks drivertest.Hooks) drivertest.Hooks {
	hooks.FailConnect = func() { f.current().aimAtClosedPort() }
	hooks.FailPing = func() { _ = f.current().Close() }
	return hooks
}

// releaseTrackedApp makes the shared runner forget appID. The runner
// keeps the last launched app as its target across connections, and the
// suite's install cases remove that app, so a later test that needs no
// app would otherwise fail on the dead target.
func (f *iosE2EFixture) releaseTrackedApp(appID string) {
	d := f.newDriver()
	ctx, cancel := context.WithTimeout(context.Background(), iosE2ECommandLimit)
	defer cancel()
	if err := d.Connect(ctx); err != nil {
		return
	}
	defer func() { _ = d.Disconnect() }()
	_ = d.StopApp(ctx, appID)
}

// faultConn is the client the driver talks through. It embeds the real
// client and, after aimAtClosedPort, redirects Connect to a client
// whose port has no listener.
type faultConn struct {
	*transport.TCPClient

	mu     sync.Mutex
	closed *transport.TCPClient
}

func (c *faultConn) aimAtClosedPort() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = transport.NewTCPClient("127.0.0.1", freeLocalPort())
}

func (c *faultConn) Connect(ctx context.Context) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed != nil {
		return closed.Connect(ctx)
	}
	return c.TCPClient.Connect(ctx)
}

// freeLocalPort returns a loopback TCP port with no listener, by
// binding port zero and releasing it.
func freeLocalPort() int {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("find a free local port: %v", err))
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// simulatorDemoHooks wires the demo app fixtures for the simulator leg:
// launchable, install from ATOMYX_IOS_INSTALL_APP and
// ATOMYX_IOS_INSTALL_APP_ID, clearable (iOS clears by uninstalling and
// reinstalling from the install source), and FocusInput through the
// app's "--route" launch argument, which avoids the system prompt a
// custom URL scheme raises.
func simulatorDemoHooks(t *testing.T, udid string) drivertest.Hooks {
	t.Helper()
	app := os.Getenv("ATOMYX_IOS_INSTALL_APP")
	appID := os.Getenv("ATOMYX_IOS_INSTALL_APP_ID")
	if app == "" || appID == "" {
		drivertest.SkipBecause(t, drivertest.SkipRequiredEnvUnset,
			"set ATOMYX_IOS_INSTALL_APP (the built Runner.app path) and ATOMYX_IOS_INSTALL_APP_ID (its bundle id)")
	}
	if _, err := runSimctl(t, "install", udid, app); err != nil {
		t.Fatalf("install the demo app on the simulator: %v", err)
	}
	return drivertest.Hooks{
		LaunchableAppID: appID,
		ClearableAppID:  appID,
		Install:         &drivertest.InstallFixture{Source: app, AppID: appID},
		FocusInput: func(t *testing.T, d driver.Driver) {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), iosE2ECommandLimit)
			defer cancel()
			if err := d.LaunchApp(ctx, appID, driver.LaunchArgs{}); err != nil {
				t.Fatalf("launch %s before opening the link: %v", appID, err)
			}
			if _, err := runSimctl(t, "launch", "--terminate-running-process", udid, appID, "--route", iosE2EFocusRoute); err != nil {
				t.Fatalf("relaunch %s on %s: %v", appID, iosE2EFocusRoute, err)
			}
			drivertest.AwaitFocusedInput(t, d, iosE2EFocusInput)
		},
	}
}

func runSimctl(t *testing.T, args ...string) (process.Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), iosE2ECommandLimit)
	defer cancel()
	return process.NewExec().Run(ctx, "xcrun", append([]string{"simctl"}, args...)...)
}
