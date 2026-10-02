package runnerprovision_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/cli"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/process"
)

// TestRunScript_PhysicalDeviceWithoutADevelopmentTeam_PrintsTheSigningRemedy
// drives the real driver factory and provisioner through the CLI with
// no development team configured, and asserts the signing remedy
// reaches stderr instead of a generic launch failure.
func TestRunScript_PhysicalDeviceWithoutADevelopmentTeam_PrintsTheSigningRemedy(t *testing.T) {
	t.Setenv("ATOMYX_DEV_TEAM", "")
	t.Setenv("ATOMYX_CODE_SIGN_STYLE", "")

	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "AtomyxDriver.xcodeproj"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(t.TempDir(), "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := process.NewFake()
	background := process.NewFakeBackground()
	provisioner := device.NewDriverProvisioner(device.DriverProvisionerDeps{
		Runner: runner, Background: background, IosProjectDir: projectDir,
	})
	var stdout, stderr bytes.Buffer
	deps := cli.Deps{
		Probe: device.NewStaticProbe([]device.Descriptor{
			{ID: "DEV-1", Platform: device.PlatformIOS, Kind: device.KindDevice, State: device.StateOnline},
		}),
		Factory: device.NewDriverFactory(device.DriverFactoryDeps{
			Runner: runner, Background: background, Provisioner: provisioner,
		}),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	code := cli.RunScript(context.Background(), deps, map[string]cli.FlagValue{
		"--file": scriptPath, "--platform": "ios", "--device": "DEV-1",
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ATOMYX_DEV_TEAM") {
		t.Fatalf("stderr = %q, want the ATOMYX_DEV_TEAM remedy", stderr.String())
	}
	if !strings.Contains(stderr.String(), "AGENT_UNREACHABLE") {
		t.Fatalf("stderr = %q, want the stable error code", stderr.String())
	}
	if got := background.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want no process launched without a development team", got)
	}
}
