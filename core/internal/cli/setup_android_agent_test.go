package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/setup"
)

// fixedProbe answers every scan with the same devices, unlike a static
// probe that hands out one snapshot per scan.
type fixedProbe []device.Descriptor

func (p fixedProbe) Scan(context.Context) []device.Descriptor { return p }

type fakeTools map[string]string

func (f fakeTools) Locate(name string) (string, bool) {
	path, ok := f[name]
	return path, ok
}

// fakeAndroidHost is a scripted agent. Installing, enabling and
// reinstalling flip the state the way the real tooling does.
type fakeAndroidHost struct {
	apkFound     bool
	installed    bool
	agentEnabled bool
	others       []string
	ready        bool
	axConnected  bool
	agentProto   int
	hostProto    int
	calls        []string
}

func (f *fakeAndroidHost) ResolveAgentAPK() (string, bool) { return "/build/app-debug.apk", f.apkFound }

func (f *fakeAndroidHost) AgentInstalled(context.Context, string) (bool, error) {
	return f.installed, nil
}

func (f *fakeAndroidHost) InstallAgent(context.Context, string) error {
	f.calls = append(f.calls, "install")
	f.installed, f.agentEnabled, f.ready, f.axConnected = true, true, true, true
	return nil
}

func (f *fakeAndroidHost) ReinstallAgent(context.Context, string) error {
	f.calls = append(f.calls, "reinstall")
	f.agentProto = f.hostProto
	return nil
}

func (f *fakeAndroidHost) AccessibilityServices(context.Context, string) (bool, []string, error) {
	return f.agentEnabled, f.others, nil
}

func (f *fakeAndroidHost) EnableAgentAccessibility(context.Context, string) error {
	f.calls = append(f.calls, "enable")
	f.agentEnabled, f.axConnected = true, true
	return nil
}

func (f *fakeAndroidHost) AgentHealth(context.Context, string) (bool, bool, error) {
	return f.ready, f.axConnected, nil
}

func (f *fakeAndroidHost) AgentProtocol(context.Context, string) (int, int, error) {
	return f.agentProto, f.hostProto, nil
}

type setupReport struct {
	OK    bool `json:"ok"`
	Steps []struct{ ID, Status, Message, Remedy string }
}

func runAndroidSetup(t *testing.T, host setup.AndroidAgentHost, tools setup.ToolLocator, flags map[string]FlagValue) (int, setupReport) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	deps.Probe = fixedProbe(heldAndroid[:1])
	deps.AndroidAgent = host
	deps.Tools = tools
	flags["--json"] = true
	code := Setup(context.Background(), deps, flags)
	var report setupReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return code, report
}

func statuses(report setupReport) string {
	var parts []string
	for _, s := range report.Steps {
		parts = append(parts, s.ID+"="+s.Status)
	}
	return strings.Join(parts, " ")
}

func TestAndroidSetupWithoutAdbSkipsEverythingThatNeedsIt(t *testing.T) {
	code, report := runAndroidSetup(t, &fakeAndroidHost{apkFound: true, hostProto: 1}, fakeTools{}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"})

	want := "android-adb=manual android-device=skipped android-apk=skipped"
	if code != 1 || statuses(report) != want {
		t.Errorf("code = %d, steps = %s", code, statuses(report))
	}
}

func TestProbeDeviceStatesMapsEveryAdbState(t *testing.T) {
	probe := fixedProbe{
		{ID: "on", Platform: device.PlatformAndroid, State: device.StateOnline},
		{ID: "off", Platform: device.PlatformAndroid, State: device.StateOffline},
		{ID: "auth", Platform: device.PlatformAndroid, State: device.StateUnauthorized},
		{ID: "boot", Platform: device.PlatformAndroid, State: device.StateUnavailable},
		{ID: "phone", Platform: device.PlatformIOS, State: device.StateOnline},
	}
	cases := map[string]string{
		"on": setup.DeviceOnline, "off": setup.DeviceOffline, "auth": setup.DeviceUnauthorized,
		"boot": setup.DeviceUnavailable, "gone": setup.DeviceAbsent, "phone": setup.DeviceAbsent,
	}
	for serial, want := range cases {
		if got := (probeDeviceStates{probe: probe}).DeviceState(context.Background(), serial); got != want {
			t.Errorf("%s: %s, want %s", serial, got, want)
		}
	}
	if got := (probeDeviceStates{}).DeviceState(context.Background(), "on"); got != setup.DeviceAbsent {
		t.Errorf("no probe: %s", got)
	}
}

func TestPathLocatorFindsOnlyToolsOnThePath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, found := (pathLocator{}).Locate("atomyx-no-such-tool"); found {
		t.Error("a tool that is not on PATH was reported found")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "atomyx-fake-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if path, found := (pathLocator{}).Locate("atomyx-fake-tool"); !found || path != tool {
		t.Errorf("Locate = %q, %v, want %q, true", path, found, tool)
	}
}
