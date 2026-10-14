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
	return code, decodeSetupReport(t, stdout.Bytes(), stderr.String(), code)
}

func decodeSetupReport(t *testing.T, stdout []byte, stderr string, code int) setupReport {
	t.Helper()
	var report setupReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("exit code %d, stdout is not JSON: %v\n%s\nstderr: %s", code, err, stdout, stderr)
	}
	return report
}

func statuses(report setupReport) string {
	var parts []string
	for _, s := range report.Steps {
		parts = append(parts, s.ID+"="+s.Status)
	}
	return strings.Join(parts, " ")
}

func TestAndroidSetupRegistersTheStepsInOrder(t *testing.T) {
	host := &fakeAndroidHost{apkFound: true, installed: true, agentEnabled: true, ready: true, axConnected: true, agentProto: 1, hostProto: 1}
	code, report := runAndroidSetup(t, host, fakeTools{"adb": "/sdk/adb"}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"})

	want := "android-adb=ok android-device=ok android-apk=ok android-agent-installed=ok android-accessibility=ok android-agent-health=ok android-agent-protocol=ok"
	if code != 0 || !report.OK || statuses(report) != want {
		t.Fatalf("code = %d, steps = %s, want 0 and %s", code, statuses(report), want)
	}
}

func TestAndroidSetupFixModeInstallsEnablesAndReinstallsOnce(t *testing.T) {
	host := &fakeAndroidHost{apkFound: true, others: []string{"com.a/.One"}, agentProto: 0, hostProto: 1}
	code, report := runAndroidSetup(t, host, fakeTools{"adb": "/sdk/adb"}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"})

	if code != 0 {
		t.Fatalf("code = %d, steps = %s", code, statuses(report))
	}
	if got := strings.Join(host.calls, ","); got != "install,reinstall" {
		t.Errorf("repairs = %s, want install then one reinstall", got)
	}
	if report.Steps[3].Status != "fixed" || report.Steps[6].Status != "fixed" {
		t.Errorf("steps = %s", statuses(report))
	}
}

func TestAndroidSetupCheckModeNeverRepairs(t *testing.T) {
	host := &fakeAndroidHost{apkFound: true, others: []string{"com.a/.One"}, hostProto: 1}
	code, report := runAndroidSetup(t, host, fakeTools{"adb": "/sdk/adb"}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--check": true})

	if code != 1 || len(host.calls) != 0 {
		t.Fatalf("code = %d, repairs = %v", code, host.calls)
	}
	want := "android-adb=ok android-device=ok android-apk=ok android-agent-installed=manual android-accessibility=skipped android-agent-health=skipped android-agent-protocol=skipped"
	if statuses(report) != want {
		t.Errorf("steps = %s, want %s", statuses(report), want)
	}
}

func TestAndroidSetupWithoutAdbSkipsEverythingThatNeedsIt(t *testing.T) {
	code, report := runAndroidSetup(t, &fakeAndroidHost{apkFound: true, hostProto: 1}, fakeTools{}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"})

	want := "android-adb=manual android-device=skipped android-apk=skipped android-agent-installed=skipped android-accessibility=skipped android-agent-health=skipped android-agent-protocol=skipped"
	if code != 1 || statuses(report) != want {
		t.Errorf("code = %d, steps = %s", code, statuses(report))
	}
}

func TestAndroidSetupMissingApkDoesNotBlockAnInstalledAgent(t *testing.T) {
	host := &fakeAndroidHost{installed: true, agentEnabled: true, ready: true, axConnected: true, agentProto: 1, hostProto: 1}
	code, report := runAndroidSetup(t, host, fakeTools{"adb": "/sdk/adb"}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554"})

	want := "android-adb=ok android-device=ok android-apk=manual android-agent-installed=ok android-accessibility=ok android-agent-health=ok android-agent-protocol=ok"
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

func TestAndroidSetupWithoutToolingReportsItInsteadOfPanicking(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	deps.Tools = fakeTools{"adb": "/sdk/adb"}
	deps.Probe = fixedProbe(heldAndroid[:1])
	flags := map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--json": true, "--check": true}

	if code := Setup(context.Background(), deps, flags); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "no Android device tooling") {
		t.Errorf("stdout = %s", stdout.String())
	}
}

func TestAndroidSetupCheckReportsTheAgentStateExactly(t *testing.T) {
	cases := []struct {
		name    string
		host    *fakeAndroidHost
		stepID  string
		message string
	}{
		{
			"protocol versions keep their sides",
			&fakeAndroidHost{apkFound: true, installed: true, agentEnabled: true, ready: true, axConnected: true, agentProto: 0, hostProto: 1},
			"android-agent-protocol", "version 0 but this host requires version 1",
		},
		{
			"ready and connected are not interchangeable",
			&fakeAndroidHost{apkFound: true, installed: true, agentEnabled: true, ready: false, axConnected: true, agentProto: 1, hostProto: 1},
			"android-agent-health", "not ready",
		},
		{
			"other services are listed",
			&fakeAndroidHost{apkFound: true, installed: true, others: []string{"com.a/.One"}, agentProto: 1, hostProto: 1},
			"android-accessibility", "services: com.a/.One.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, report := runAndroidSetup(t, tc.host, fakeTools{"adb": "/sdk/adb"}, map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--check": true})
			for _, step := range report.Steps {
				if step.ID == tc.stepID {
					if !strings.Contains(step.Message, tc.message) {
						t.Errorf("%s message = %q, want it to contain %q", step.ID, step.Message, tc.message)
					}
					return
				}
			}
			t.Errorf("step %s was not reported", tc.stepID)
		})
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
