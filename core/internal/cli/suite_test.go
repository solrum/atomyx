package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/orchestra"
)

func TestResolveSuiteSelectorRejectsMutualExclusion(t *testing.T) {
	name := "smoke"
	_, err := resolveSuiteSelector(&name, map[string]FlagValue{"--tag": []string{"x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("got %v, want a mutual-exclusion error", err)
	}
}

func TestResolveSuiteSelectorRequiresSomeSelector(t *testing.T) {
	_, err := resolveSuiteSelector(nil, map[string]FlagValue{}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires either a suite name") {
		t.Fatalf("got %v, want a missing-selector error", err)
	}
}

func TestResolveSuiteSelectorNamedSuiteFromConfig(t *testing.T) {
	tags := []string{"smoke"}
	globs := []string{"flows/**/*.yml"}
	loaded := &config.LoadedConfig{Config: config.AtomyxConfig{
		Suites: &map[string]config.SuiteEntry{
			"smoke-suite": {Tags: &tags, Globs: &globs},
		},
	}}
	name := "smoke-suite"

	sel, err := resolveSuiteSelector(&name, map[string]FlagValue{}, loaded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sel.Tags) != 1 || sel.Tags[0] != "smoke" {
		t.Errorf("tags: got %v, want [smoke]", sel.Tags)
	}
	if len(sel.Globs) != 1 || sel.Globs[0] != "flows/**/*.yml" {
		t.Errorf("globs: got %v, want [flows/**/*.yml]", sel.Globs)
	}
}

func TestResolveSuiteSelectorUnknownNamedSuiteListsAvailableSorted(t *testing.T) {
	loaded := &config.LoadedConfig{Config: config.AtomyxConfig{
		Suites: &map[string]config.SuiteEntry{"zeta": {}, "alpha": {}},
	}}
	name := "missing"

	_, err := resolveSuiteSelector(&name, map[string]FlagValue{}, loaded)
	if err == nil || !strings.Contains(err.Error(), `suite "missing" is not defined in the project config`) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "available suites: alpha, zeta") {
		t.Errorf("available suites not listed sorted: %v", err)
	}
}

func TestResolveSuiteSelectorUnknownNamedSuiteWithNoneDeclared(t *testing.T) {
	name := "missing"
	_, err := resolveSuiteSelector(&name, map[string]FlagValue{}, nil)
	if err == nil || !strings.Contains(err.Error(), "available suites: (none)") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveSuiteSelectorAdhocTagsAndGlobs(t *testing.T) {
	sel, err := resolveSuiteSelector(nil, map[string]FlagValue{
		"--tag":  []string{"smoke", "critical"},
		"--glob": []string{"*.yml"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sel.Tags) != 2 || sel.Tags[0] != "smoke" || sel.Tags[1] != "critical" {
		t.Errorf("tags: got %v", sel.Tags)
	}
	if len(sel.Globs) != 1 || sel.Globs[0] != "*.yml" {
		t.Errorf("globs: got %v", sel.Globs)
	}
}

// TestRunSuiteDeviceFreeMixedResult runs a suite of four discovered
// files, including a passing script, a failing script, a scenario wrapping a
// passing child script, and that child script matched again on its
// own, through an injected device.StaticProbe and
// device.MockDriverFactory, with no adb, xcrun, or network access.
// It asserts the rendered SuiteResult shape (passed, failed, total,
// every entry ran) and, with --report to a t.TempDir(), that
// report.json's top-level "kind" is "suite".
func TestRunSuiteDeviceFreeMixedResult(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, dir, "pass.yml", "- sleep: 1\n")
	writeFile(t, dir, "fail.yml", "- assertExpr: { expr: '1 == 2' }\n")
	writeFile(t, dir, "child.yml", "- sleep: 1\n")
	writeFile(t, dir, "scenario.yml", "name: Nested\nscripts:\n  - child.yml\n")

	reportDir := t.TempDir()

	clock := orchestra.NewFakeClock(0)
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock}),
		Clock:   clock,
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunSuite(context.Background(), deps, map[string]FlagValue{
		"--glob":       []string{"*.yml"},
		"--report":     "json",
		"--report-dir": reportDir,
	}, nil)

	// child.yml, fail.yml, pass.yml, scenario.yml: 3 pass, 1 fails.
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1 (fail.yml should fail the suite); stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "FAIL: suite\n") {
		t.Errorf("stdout missing FAIL heading: %q", out)
	}
	if !strings.Contains(out, "3 of 4 flows passed") {
		t.Errorf("stdout missing passed/total line: %q", out)
	}
	for _, name := range []string{"child.yml", "fail.yml", "pass.yml", "scenario.yml"} {
		if !strings.Contains(out, name) {
			t.Errorf("stdout missing entry %q: %q", name, out)
		}
	}
	if !strings.Contains(out, "[scenario] ") {
		t.Errorf("stdout missing the scenario entry's kind tag: %q", out)
	}
	if !strings.Contains(out, "[script] ") {
		t.Errorf("stdout missing a script entry's kind tag: %q", out)
	}

	bundleJSON := findReportJSON(t, reportDir)
	var decoded map[string]any
	if err := json.Unmarshal(bundleJSON, &decoded); err != nil {
		t.Fatalf("invalid report.json: %v", err)
	}
	if decoded["kind"] != "suite" {
		t.Errorf("kind: got %#v, want \"suite\"", decoded["kind"])
	}
	result, ok := decoded["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing or wrong shape: %#v", decoded["result"])
	}
	if result["total"] != float64(4) || result["passed"] != float64(3) || result["failed"] != float64(1) {
		t.Errorf("result totals: got %#v", result)
	}
}

// TestRunSuiteNoMatchExitsUsageError confirms an empty resolved
// selection exits 2 with the TypeScript CLI's own "nothing to run"
// message, rather than a silent no-op.
func TestRunSuiteNoMatchExitsUsageError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunSuite(context.Background(), deps, map[string]FlagValue{"--glob": []string{"*.yml"}}, nil)
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no flows matched the selector") {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
