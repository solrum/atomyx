package mcp

import (
	"context"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/runfolder"
)

func TestRunStartedWhileTheDeviceIsReleasedRecordsTheAgentVersionsOnReacquire(t *testing.T) {
	f := newLifecycleFixture(t)
	want := driver.AgentInfo{ProtocolVersion: 4, AgentVersion: "2.0.1"}
	f.factory.onBuild = func(_ context.Context, d *lifecycleDriver) { d.Agent = want }
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)
	if _, err := f.runs.Start(context.Background(), "smoke", nil, f.devices.SelectedDescriptor()); err != nil {
		t.Fatal(err)
	}

	if err := ensureErr(f.devices); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	metas, _, err := runfolder.NewReader(f.root).List()
	if err != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v), want exactly one", len(metas), err)
	}
	if got := metas[0].Host; got == nil || got.ProtocolVersion != want.ProtocolVersion || got.AgentVersion != want.AgentVersion {
		t.Fatalf("host = %+v, want the versions %+v of the reacquired driver", got, want)
	}
}

func TestRecordAgentKeepsTheVersionsAnOpenRunAlreadyRecorded(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Start(context.Background(), "smoke", nil, f.devices.SelectedDescriptor()); err != nil {
		t.Fatal(err)
	}

	f.runs.RecordAgent(driver.AgentInfo{ProtocolVersion: 1, AgentVersion: "first"})
	f.runs.RecordAgent(driver.AgentInfo{ProtocolVersion: 2, AgentVersion: "second"})

	metas, _, err := runfolder.NewReader(f.root).List()
	if err != nil || len(metas) != 1 {
		t.Fatalf("run folders = %d (%v), want exactly one", len(metas), err)
	}
	if got := metas[0].Host; got == nil || got.AgentVersion != "first" {
		t.Fatalf("host = %+v, want the versions recorded at the start kept", got)
	}
}
