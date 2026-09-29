package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
)

func testDeps(stdout, stderr *bytes.Buffer) Deps {
	return Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  stdout,
		Stderr:  stderr,
	}
}

func TestRunNoArgsPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "USAGE") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestRunVersionPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"version"}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "atomyx ") || !strings.Contains(stdout.String(), "(commit ") {
		t.Errorf("stdout: got %q, want an \"atomyx <version> (commit ...)\" line", stdout.String())
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"bogus"}, testDeps(&stdout, &stderr))
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `Unknown command: "bogus"`) {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

func TestRunSuiteShortcutDispatchesToDriverSuite(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"suite", "smoke"}, testDeps(&stdout, &stderr))
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `suite "smoke" is not defined in the project config`) {
		t.Errorf("stderr: got %q, want it to reach RunSuite's named-suite lookup", stderr.String())
	}
}

func TestRunDevicesShortcutDispatchesToListDevices(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"devices", "--json"}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"devices"`) {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestRunShortcutDispatchesToDriverRun(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--file", scriptPath}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestRunDriverHelpPrintsModuleHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"driver"}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "atomyx driver") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestRunSuiteShortcutHelpFlagPrintsHelpInsteadOfSuiteLookup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"suite", "-h"}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "atomyx driver suite") {
		t.Errorf("stdout: got %q, want the suite command's help text", stdout.String())
	}
	if strings.Contains(stderr.String(), "is not defined in the project config") {
		t.Errorf("stderr: got %q, want -h to short-circuit before the suite lookup", stderr.String())
	}
}

func TestRunDriverSuiteHelpFlagPrintsHelpInsteadOfSuiteLookup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"driver", "suite", "-h"}, testDeps(&stdout, &stderr))
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "atomyx driver suite") {
		t.Errorf("stdout: got %q, want the suite command's help text", stdout.String())
	}
	if strings.Contains(stderr.String(), "is not defined in the project config") {
		t.Errorf("stderr: got %q, want -h to short-circuit before the suite lookup", stderr.String())
	}
}

func TestRunDriverSuiteDispatchesToRunSuite(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"driver", "suite", "smoke"}, testDeps(&stdout, &stderr))
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `suite "smoke" is not defined in the project config`) {
		t.Errorf("stderr: got %q, want it to reach RunSuite's named-suite lookup", stderr.String())
	}
}
