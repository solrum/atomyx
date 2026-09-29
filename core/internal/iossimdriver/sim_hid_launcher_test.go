package iossimdriver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
)

// setSimHidBinEnv points ATOMYX_SIM_HID_BIN at path for the duration
// of the test, restoring whatever the environment held before. Most
// Launch tests only care that resolveSimHidBinary short-circuits to a
// fake path, so the value need not name a real file.
func setSimHidBinEnv(t *testing.T, path string) {
	t.Helper()
	saved, had := os.LookupEnv("ATOMYX_SIM_HID_BIN")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("ATOMYX_SIM_HID_BIN", saved)
		} else {
			_ = os.Unsetenv("ATOMYX_SIM_HID_BIN")
		}
	})
	_ = os.Setenv("ATOMYX_SIM_HID_BIN", path)
}

func TestSimHidLauncher_Launch_ValidHandshake_ReturnsPortAndLeavesProcessRunning(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen","port":19876,"transport":"ws-input"}` + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	result, err := launcher.Launch(context.Background(), "UDID-VALID")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if result.Port != 19876 {
		t.Fatalf("Launch() Port = %d, want 19876", result.Port)
	}
	if proc.Killed() {
		t.Fatal("Launch() killed the process on a valid handshake, want it left running")
	}

	invocations := bg.Invocations()
	if len(invocations) != 1 {
		t.Fatalf("Invocations() = %+v, want exactly one Start call", invocations)
	}
	if invocations[0].Args[0] != "--udid" || invocations[0].Args[1] != "UDID-VALID" {
		t.Fatalf("Start() args = %v, want [--udid UDID-VALID]", invocations[0].Args)
	}

	if err := result.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if !proc.Killed() {
		t.Fatal("Kill() did not mark the process killed")
	}
}

func TestSimHidLauncher_Launch_AcceptsZeroPort(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen","port":0,"transport":"ws-input"}` + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	result, err := launcher.Launch(context.Background(), "UDID-ZERO-PORT")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if result.Port != 0 {
		t.Fatalf("Launch() Port = %d, want 0 (present but zero)", result.Port)
	}
}

func TestSimHidLauncher_Launch_InvalidJSON_FailsAndKillsProcess(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte("not-json\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err := launcher.Launch(context.Background(), "UDID-BAD-JSON")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrInvalidHandshake {
		t.Fatalf("Launch() error = %v, want SimHidErrInvalidHandshake", err)
	}
	if !proc.Killed() {
		t.Fatal("Launch() did not kill the process on invalid handshake JSON")
	}
}

func TestSimHidLauncher_Launch_WrongTransport_FailsAndKillsProcess(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen","port":1234,"transport":"wrong-transport"}` + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err := launcher.Launch(context.Background(), "UDID-WRONG-TRANSPORT")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrUnexpectedHandshake {
		t.Fatalf("Launch() error = %v, want SimHidErrUnexpectedHandshake", err)
	}
	if !proc.Killed() {
		t.Fatal("Launch() did not kill the process on an unexpected handshake")
	}
}

func TestSimHidLauncher_Launch_MissingPort_FailsAndKillsProcess(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen","transport":"ws-input"}` + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err := launcher.Launch(context.Background(), "UDID-NO-PORT")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrUnexpectedHandshake {
		t.Fatalf("Launch() error = %v, want SimHidErrUnexpectedHandshake", err)
	}
}

func TestSimHidLauncher_Launch_ExitsBeforeHandshake_FailsWithExitedEarly(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	proc.Exit(1)

	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err := launcher.Launch(context.Background(), "UDID-EARLY-EXIT")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrExitedEarly {
		t.Fatalf("Launch() error = %v, want SimHidErrExitedEarly", err)
	}
}

func TestSimHidLauncher_Launch_PartialLineBeforeExit_FailsWithExitedEarly(t *testing.T) {
	setSimHidBinEnv(t, "fake-atomyx-sim-hid")
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen"`)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	proc.Exit(0)

	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err := launcher.Launch(context.Background(), "UDID-PARTIAL-LINE")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrExitedEarly {
		t.Fatalf("Launch() error = %v, want SimHidErrExitedEarly", err)
	}
}

func TestSimHidLauncher_Launch_BinaryNotFound_ReturnsErrorWithoutSpawning(t *testing.T) {
	saved, hadSaved := os.LookupEnv("ATOMYX_SIM_HID_BIN")
	if hadSaved {
		defer func() { _ = os.Setenv("ATOMYX_SIM_HID_BIN", saved) }()
	} else {
		defer func() { _ = os.Unsetenv("ATOMYX_SIM_HID_BIN") }()
	}
	_ = os.Unsetenv("ATOMYX_SIM_HID_BIN")

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	bg := process.NewFakeBackground()
	launcher := iossimdriver.NewSimHidLauncher(bg)
	_, err = launcher.Launch(context.Background(), "UDID-NO-BINARY")
	var launchErr *iossimdriver.SimHidLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != iossimdriver.SimHidErrBinaryNotFound {
		t.Fatalf("Launch() error = %v, want SimHidErrBinaryNotFound", err)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("Invocations() = %+v, want none when the binary cannot be resolved", bg.Invocations())
	}
}

func TestSimHidLauncher_Launch_BinEnvOverride_SpawnsExactPath(t *testing.T) {
	dir := t.TempDir()
	helperPath := filepath.Join(dir, "fake-atomyx-sim-hid")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	setSimHidBinEnv(t, helperPath)

	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	if _, err := proc.Write([]byte(`{"event":"listen","port":4242,"transport":"ws-input"}` + "\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	launcher := iossimdriver.NewSimHidLauncher(bg)
	result, err := launcher.Launch(context.Background(), "UDID-ENV-OVERRIDE")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if result.Port != 4242 {
		t.Fatalf("Launch() Port = %d, want 4242", result.Port)
	}
	invocations := bg.Invocations()
	if len(invocations) != 1 || invocations[0].Name != helperPath {
		t.Fatalf("Invocations() = %+v, want a single Start at %q", invocations, helperPath)
	}
}
