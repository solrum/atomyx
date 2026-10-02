package mcp

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/storage"
)

const contractAcquireBudgetMs int64 = 120_000

// slowBuildFactory models a device build that outlasts whatever time
// budget its context carries, such as a cold runner compile.
type slowBuildFactory struct {
	inner device.Factory
	clk   *clock.ManualClock
}

func (f slowBuildFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	f.clk.AwaitSleepers(1)
	f.clk.Advance(contractAcquireBudgetMs + 1)
	<-ctx.Done()
	if err := ctx.Err(); err != nil {
		return device.Built{}, err
	}
	return f.inner.Build(ctx, desc)
}

func TestAcquireBudgetExcludesRunnerBuild(t *testing.T) {
	t.Skip("removed by the separate prepare phase step")
	f := newLifecycleFixture(t)
	slow := f.newDevices("atomyx-mcp", slowBuildFactory{inner: f.factory, clk: f.clk})
	t.Cleanup(slow.Close)

	_, err := slow.Select(context.Background(), SelectDeviceInput{Platform: device.PlatformAndroid, ID: "A"})

	if err != nil {
		t.Fatalf("Select() error = %v, want a build longer than the acquisition budget to leave the acquire alone", err)
	}
}

func TestCheckHealthDoesNotReacquireReleasedDevice(t *testing.T) {
	t.Skip("removed by the session transitions step")
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	f.devices.CheckHealth(context.Background())

	if got := f.factory.builds(); got != 1 {
		t.Fatalf("builds = %d, want 1 because check_health must not take a released device again", got)
	}
	if f.leaseHeld() {
		t.Fatal("the device lease is held after check_health on a released device")
	}
}

func TestSelectFailureKeepsCurrentDevice(t *testing.T) {
	t.Skip("removed by the session transitions step")
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	other := f.newDevices("atomyx run", newLifecycleFactory())
	t.Cleanup(other.Close)
	busy := SelectDeviceInput{Platform: device.PlatformAndroid, ID: "B"}
	if _, err := other.Select(context.Background(), busy); err != nil {
		t.Fatal(err)
	}

	if _, err := f.devices.Select(context.Background(), busy); err == nil {
		t.Fatal("Select() of a device another process holds succeeded")
	}

	current := f.devices.Current()
	if current == nil || current.ID != "A" {
		t.Fatalf("Current() = %+v, want the working device A kept after the failed select", current)
	}
	if !f.leaseHeld() {
		t.Fatal("the lease of the working device was given up by the failed select")
	}
}

func TestReacquireReportsAppRelaunch(t *testing.T) {
	t.Skip("removed by the relaunch notice step")
	f := newLifecycleFixture(t)
	f.factory.onBuild = func(_ context.Context, d *lifecycleDriver) {
		if d.factory.builds() == 0 {
			d.bundleID, d.runnerID = "com.example.app", "runner-1"
		}
	}
	cs, ctx := testServer(t, ServerDeps{
		Probe:      device.NewStaticProbe(),
		Factory:    f.factory,
		Storage:    storage.NewInMemoryStorage(),
		Clock:      f.clk,
		TimerClock: f.clk,
		Session:    session.Deps{Leases: f.leases, Clock: clock.SystemClock{}, Command: "atomyx-mcp", Role: "mcp"},
		Timeouts:   session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: contractAcquireBudgetMs},
	})
	callTool(t, cs, ctx, "select_device", map[string]any{"platform": "android", "id": "A"})
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	<-f.factory.disconnected

	result, _ := callTool(t, cs, ctx, "get_ui_tree", map[string]any{})

	if text := strings.ToLower(textOf(t, result)); !strings.Contains(text, "relaunch") {
		t.Fatalf("get_ui_tree after a re-acquire = %s, want a notice that the app was relaunched", text)
	}
}
