package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
)

const (
	fakeIdentityLine = `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: Test Person (AAAAA11111)"` + "\n"
	otherIdentity    = `  2) 456789ABCDEF0123456789ABCDEF0123456789AB "Apple Development: Other Person (BBBBB22222)"` + "\n"
)

var iosDevices = fixedProbe{
	{ID: "SIM-1", Platform: device.PlatformIOS, Name: "Simulator", Kind: device.KindSimulator, State: device.StateOnline},
	{ID: "PHONE-1", Platform: device.PlatformIOS, Name: "Phone", Kind: device.KindDevice, State: device.StateOnline},
}

// hostTools answers the host commands the iOS steps run.
func hostTools(developerDir string, identities string, identitiesErr error) HostCommand {
	return func(_ context.Context, name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "xcode-select -p":
			return developerDir + "\n", nil
		case "security find-identity -v -p codesigning":
			return identities, identitiesErr
		}
		return "", errors.New("unexpected host command " + name + " " + strings.Join(args, " "))
	}
}

func runIOSSetup(t *testing.T, flags map[string]FlagValue, tools fakeTools, host HostCommand) (int, setupReport) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	deps := setupDeps(t, &stdout, &stderr)
	deps.Probe = iosDevices
	deps.Tools = tools
	deps.HostCommand = host
	flags["--platform"] = "ios"
	flags["--json"] = true
	code := Setup(context.Background(), deps, flags)
	return code, decodeSetupReport(t, stdout.Bytes(), stderr.String(), code)
}

func TestIOSSetupSimulatorNeedsNoIproxyOrSigning(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	code, report := runIOSSetup(t, map[string]FlagValue{"--device": "SIM-1"}, fakeTools{"xcrun": "/usr/bin/xcrun"}, hostTools("/Applications/Xcode.app/Contents/Developer", "", errors.New("unused")))

	want := "ios-xcode=ok ios-iproxy=ok ios-signing-team=ok"
	if code != 0 || statuses(report) != want {
		t.Fatalf("code = %d, steps = %s, want 0 and %s", code, statuses(report), want)
	}
}

func TestIOSSetupPhysicalDeviceWithoutIproxyAndTeamIsManual(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	code, report := runIOSSetup(t, map[string]FlagValue{"--device": "PHONE-1"}, fakeTools{"xcrun": "/usr/bin/xcrun"}, hostTools("/Applications/Xcode.app/Contents/Developer", fakeIdentityLine, nil))

	want := "ios-xcode=ok ios-iproxy=manual ios-signing-team=manual"
	if code != 1 || statuses(report) != want {
		t.Fatalf("code = %d, steps = %s, want 1 and %s", code, statuses(report), want)
	}
	if !strings.Contains(report.Steps[2].Remedy, "export ATOMYX_DEV_TEAM=AAAAA11111") {
		t.Errorf("remedy = %q", report.Steps[2].Remedy)
	}
}

func TestIOSSetupPhysicalDeviceIsOkWhenEverythingIsPresent(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "BBBBB22222")
	tools := fakeTools{"xcrun": "/usr/bin/xcrun", "iproxy": "/opt/bin/iproxy"}
	code, report := runIOSSetup(t, map[string]FlagValue{"--device": "PHONE-1"}, tools, hostTools("/Applications/Xcode.app/Contents/Developer", fakeIdentityLine+otherIdentity, nil))

	if code != 0 || statuses(report) != "ios-xcode=ok ios-iproxy=ok ios-signing-team=ok" {
		t.Fatalf("code = %d, steps = %s", code, statuses(report))
	}
}

func TestIOSSetupCheckModeNeverWritesAnything(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	var commands []string
	host := func(ctx context.Context, name string, args ...string) (string, error) {
		commands = append(commands, name)
		return hostTools("/dev", fakeIdentityLine, nil)(ctx, name, args...)
	}
	runIOSSetup(t, map[string]FlagValue{"--device": "PHONE-1", "--check": true}, fakeTools{"xcrun": "/usr/bin/xcrun"}, host)

	if got := strings.Join(commands, ","); got != "xcode-select,security" {
		t.Errorf("host commands = %s, want only the two read-only queries", got)
	}
}

func TestIOSSetupWithoutADeviceChecksWhatAPhysicalOneNeeds(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	code, report := runIOSSetup(t, map[string]FlagValue{}, fakeTools{"xcrun": "/usr/bin/xcrun"}, hostTools("/dev", fakeIdentityLine, nil))

	if code != 1 || report.Steps[1].Status != "manual" || report.Steps[2].Status != "manual" {
		t.Errorf("code = %d, steps = %s", code, statuses(report))
	}
}

func TestIOSSetupWithoutAHostCommandRunnerFailsTheSigningStep(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	_, report := runIOSSetup(t, map[string]FlagValue{"--device": "PHONE-1"}, fakeTools{"xcrun": "/usr/bin/xcrun", "iproxy": "/x"}, nil)

	if report.Steps[0].Status != "manual" || report.Steps[2].Status != "failed" {
		t.Errorf("steps = %s", statuses(report))
	}
}

func TestIOSTargetClassifiesLikeTheRunnerProvisioner(t *testing.T) {
	cases := map[device.Kind]string{"": "unknown", device.KindSimulator: "simulator", device.KindDevice: "physical", device.KindEmulator: "physical"}
	for kind, want := range cases {
		got := map[int]string{0: "unknown", 1: "simulator", 2: "physical"}[int(iosTarget(setupTarget{Kind: kind}))]
		if got != want {
			t.Errorf("kind %q: %s, want %s", kind, got, want)
		}
	}
}
