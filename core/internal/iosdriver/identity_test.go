package iosdriver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dev.atomyx/internal/iosdriver"
)

func TestExpectedRunner_Verify(t *testing.T) {
	sim := iosdriver.ExpectedRunner{UDID: "SIM-1", Kind: iosdriver.KindSimulator}
	simManaged := iosdriver.ExpectedRunner{UDID: "SIM-1", Kind: iosdriver.KindSimulator, RunnerID: "r-1"}
	dev := iosdriver.ExpectedRunner{UDID: "DEV-1", Kind: iosdriver.KindDevice}
	devManaged := iosdriver.ExpectedRunner{UDID: "DEV-1", Kind: iosdriver.KindDevice, RunnerID: "r-1"}

	tests := []struct {
		name string
		want iosdriver.ExpectedRunner
		got  iosdriver.RunnerIdentity
		more iosdriver.IdentityVerdict
	}{
		{"simulator, same simulator, no runner id is unmanaged", sim, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-1"}, iosdriver.IdentityUnmanaged},
		{"simulator udid compares case-insensitively", sim, iosdriver.RunnerIdentity{SimulatorUDID: "sim-1"}, iosdriver.IdentityUnmanaged},
		{"simulator, other simulator", sim, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-2"}, iosdriver.IdentityMismatch},
		{"simulator, legacy runner reporting nothing", sim, iosdriver.RunnerIdentity{}, iosdriver.IdentityLegacy},
		{"simulator, runner with an id but no runner id requested", sim, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-1", RunnerID: "x"}, iosdriver.IdentityMatch},
		{"simulator, matching runner id", simManaged, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-1", RunnerID: "r-1"}, iosdriver.IdentityMatch},
		{"simulator, different runner id", simManaged, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-1", RunnerID: "r-2"}, iosdriver.IdentityMismatch},
		{"simulator, runner id requested but runner has none", simManaged, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-1"}, iosdriver.IdentityMismatch},
		{"simulator, matching runner id on the wrong simulator", simManaged, iosdriver.RunnerIdentity{SimulatorUDID: "SIM-2", RunnerID: "r-1"}, iosdriver.IdentityMismatch},
		{"device, matching target udid", dev, iosdriver.RunnerIdentity{TargetUDID: "DEV-1"}, iosdriver.IdentityMatch},
		{"device, other target udid", dev, iosdriver.RunnerIdentity{TargetUDID: "DEV-2"}, iosdriver.IdentityMismatch},
		{"device, reporting neither", dev, iosdriver.RunnerIdentity{}, iosdriver.IdentityLegacy},
		{"device, matching runner id", devManaged, iosdriver.RunnerIdentity{RunnerID: "r-1"}, iosdriver.IdentityMatch},
		{"device, different runner id", devManaged, iosdriver.RunnerIdentity{RunnerID: "r-2", TargetUDID: "DEV-1"}, iosdriver.IdentityMismatch},
		{"device, matching runner id with another target udid", devManaged, iosdriver.RunnerIdentity{RunnerID: "r-1", TargetUDID: "DEV-2"}, iosdriver.IdentityMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdict, reason := tc.want.Verify(tc.got)
			if verdict != tc.more {
				t.Fatalf("Verify() = %v (%q), want %v", verdict, reason, tc.more)
			}
			if verdict != iosdriver.IdentityMatch && verdict != iosdriver.IdentityUnmanaged && reason == "" {
				t.Fatal("a mismatch must carry a reason")
			}
		})
	}
}

func TestProbeRunnerIdentity_ReadsReportedFields(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, `"runnerId":"r-9","targetUdid":"DEV-9","simulatorUdid":"SIM-9"`)
	defer cleanup()

	got, ok := iosdriver.ProbeRunnerIdentity(context.Background(), port, time.Second)
	if !ok {
		t.Fatal("ProbeRunnerIdentity() ok = false, want true for a listener answering ping")
	}
	want := iosdriver.RunnerIdentity{RunnerID: "r-9", TargetUDID: "DEV-9", SimulatorUDID: "SIM-9"}
	if got != want {
		t.Fatalf("ProbeRunnerIdentity() = %+v, want %+v", got, want)
	}
}

func TestProbeRunnerIdentity_NonAnsweringListenerIsNotARunner(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, false, "")
	defer cleanup()

	if _, ok := iosdriver.ProbeRunnerIdentity(context.Background(), port, time.Second); ok {
		t.Fatal("ProbeRunnerIdentity() ok = true, want false for a listener that never answers")
	}
}

func TestCheckRunnerIdentity_MismatchReturnsIdentityError(t *testing.T) {
	port, cleanup := startFakeRunnerListener(t, true, `"simulatorUdid":"SIM-2"`)
	defer cleanup()

	found, _, err := iosdriver.CheckRunnerIdentity(context.Background(), port, time.Second,
		iosdriver.ExpectedRunner{UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if !found {
		t.Fatal("found = false, want true because a runner answered")
	}
	var idErr *iosdriver.RunnerIdentityError
	if !errors.As(err, &idErr) {
		t.Fatalf("err = %v, want *RunnerIdentityError", err)
	}
}

// pongReply builds a ping reply frame whose data object carries
// identityFields, the inner part of the object, after the pong flag.
func pongReply(id int64, identityFields string) string {
	if identityFields == "" {
		return fmt.Sprintf(`{"id":%d,"ok":true,"data":{"pong":true}}`+"\n", id)
	}
	return fmt.Sprintf(`{"id":%d,"ok":true,"data":{"pong":true,%s}}`+"\n", id, identityFields)
}
