package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storageroot"
	"dev.atomyx/internal/wire"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// stagedMockFactory builds a mock-backed device whose hierarchy already
// presents a ready-for-input surface, so a script driven through
// RunScript can type without the driver reporting an empty tree. It is
// the RunScript-level equivalent of staging a hierarchy on a mock the
// test constructs directly.
type stagedMockFactory struct {
	tree wire.TreeNodeWire
}

func (f stagedMockFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f stagedMockFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	drv.StageHierarchy(f.tree)
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

// focusedInputTree is a minimal hierarchy with a single focused input
// field, enough for a type action's focus-readiness gate to resolve.
func focusedInputTree() wire.TreeNodeWire {
	focused := true
	return wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root", "bounds": "0,0,400,800"},
		Children: []wire.TreeNodeWire{{
			Attributes: map[string]string{"id": "field", "bounds": "0,100,400,150"},
			Focused:    &focused,
		}},
	}
}

func TestPrintTextResultPassingScript(t *testing.T) {
	result := runner.ScriptResult{
		ScriptName:  "smoke",
		Ok:          true,
		PassedSteps: 1,
		TotalSteps:  1,
		DurationMs:  42,
		Steps: []runner.StepResult{
			{StepIndex: 0, Command: "sleep", Ok: true, DurationMs: 10},
		},
		Artifacts: emptyArtifacts{},
	}
	var buf bytes.Buffer
	printTextResult(&buf, result)

	want := "\nPASS: smoke\n  1/1 steps passed (42ms)\n\n  ✓ [1] sleep (10ms)\n\n"
	if buf.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestPrintTextResultFailingScriptWithDetailAndScreenshots(t *testing.T) {
	result := runner.ScriptResult{
		ScriptName:   "smoke",
		Ok:           false,
		PassedSteps:  0,
		TotalSteps:   1,
		FailedAtStep: intPtr(0),
		DurationMs:   5,
		Steps: []runner.StepResult{
			{StepIndex: 0, Command: "tap", Ok: false, Detail: strPtr("element not found"), DurationMs: 3},
		},
		Artifacts: fakeArtifacts{shots: []runner.Screenshot{{Label: "failure"}}},
	}
	var buf bytes.Buffer
	printTextResult(&buf, result)

	want := "\nFAIL: smoke\n  0/1 steps passed (5ms)\n\n" +
		"  ✗ [1] tap (3ms): element not found\n\n" +
		"  Failed at step 1\n" +
		"\n  Screenshots: failure\n\n"
	if buf.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestToJSONScriptResultShapeAndOmission(t *testing.T) {
	result := runner.ScriptResult{
		ScriptName:  "smoke",
		Ok:          true,
		PassedSteps: 1,
		TotalSteps:  1,
		DurationMs:  7,
		Steps: []runner.StepResult{
			{StepIndex: 0, Command: "sleep", Ok: true, DurationMs: 7},
		},
		Artifacts: fakeArtifacts{shots: []runner.Screenshot{{Label: "shot", Data: []byte("12345")}}},
	}

	var buf bytes.Buffer
	if err := encodeJSON(&buf, toJSONScriptResult(result)); err != nil {
		t.Fatalf("encodeJSON: %v", err)
	}
	out := buf.String()

	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end with a trailing newline")
	}
	if strings.Contains(out, "failedAtStep") {
		t.Errorf("failedAtStep should be omitted when nil:\n%s", out)
	}
	if strings.Contains(out, `"detail"`) {
		t.Errorf("detail should be omitted when nil:\n%s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	artifacts, ok := decoded["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts missing or wrong shape: %#v", decoded["artifacts"])
	}
	screenshots, ok := artifacts["screenshots"].([]any)
	if !ok || len(screenshots) != 1 {
		t.Fatalf("screenshots missing or wrong shape: %#v", artifacts["screenshots"])
	}
	shot := screenshots[0].(map[string]any)
	if shot["label"] != "shot" {
		t.Errorf("label: got %#v", shot["label"])
	}
	if shot["sizeBytes"] != float64(5) {
		t.Errorf("sizeBytes: got %#v, want 5", shot["sizeBytes"])
	}
	if _, hasRawData := shot["data"]; hasRawData {
		t.Errorf("raw screenshot data leaked into JSON output")
	}
}

func TestResolveCliPreconditionsFlag(t *testing.T) {
	cases := []struct {
		name    string
		flags   map[string]FlagValue
		want    runner.PreconditionsOverride
		wantErr bool
	}{
		{"none", map[string]FlagValue{}, runner.PreconditionsNoOverride, false},
		{"skip value", map[string]FlagValue{"--preconditions": "skip"}, runner.PreconditionsSkip, false},
		{"force value", map[string]FlagValue{"--preconditions": "force"}, runner.PreconditionsForce, false},
		{"shorthand", map[string]FlagValue{"--skip-preconditions": true}, runner.PreconditionsSkip, false},
		{"invalid value", map[string]FlagValue{"--preconditions": "bogus"}, "", true},
		{"conflicting", map[string]FlagValue{"--skip-preconditions": true, "--preconditions": "force"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCliPreconditionsFlag(tc.flags)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildExternalEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	varFile := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(varFile, []byte(`{"FOO":"file","BAZ":"file-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	flags := map[string]FlagValue{
		"--var-file": varFile,
		"--var":      []string{"FOO=cli"},
	}
	env, err := buildExternalEnv(flags, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env["FOO"] != "cli" {
		t.Errorf("--var should win over --var-file: got %q", env["FOO"])
	}
	if env["BAZ"] != "file-only" {
		t.Errorf("--var-file value missing: got %q", env["BAZ"])
	}
}

func TestBuildExternalEnvRejectsMalformedVar(t *testing.T) {
	_, err := buildExternalEnv(map[string]FlagValue{"--var": []string{"NOVALUE"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Errorf("got %v, want a KEY=VALUE error", err)
	}
}

func TestBuildExternalEnvConfigVarsBelowCliAndFile(t *testing.T) {
	env := &config.ResolvedEnvironment{
		Name:  "staging",
		AppID: "com.example.app",
		Vars:  map[string]string{"FOO": "config", "ONLY_CONFIG": "config-only"},
	}
	got, err := buildExternalEnv(map[string]FlagValue{"--var": []string{"FOO=cli"}}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["FOO"] != "cli" {
		t.Errorf("--var should win over the config environment's vars: got %q", got["FOO"])
	}
	if got["ONLY_CONFIG"] != "config-only" {
		t.Errorf("config environment var missing: got %q", got["ONLY_CONFIG"])
	}
	if got["appId"] != "com.example.app" {
		t.Errorf("resolved environment's appId missing: got %q", got["appId"])
	}
}

func TestRunScriptMissingFileExitsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{})
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--file <path> is required") {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

func TestRunScriptInvalidPlatformExitsUsageError(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--platform": "windows",
	})
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `Unknown platform "windows"`) {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

// TestRunScriptConfigEnvProxyFlagsAreAccepted confirms --config, --env,
// and --proxy no longer fail the command outright: a run naming a
// nonexistent config path with an explicit --env fails with the
// project config's own "not found" error, never with the old
// "is not supported yet in this build." usage error, and --proxy on
// its own runs a trivial script through to completion.
func TestRunScriptConfigEnvProxyFlagsAreAccepted(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingConfigPath := filepath.Join(dir, "does-not-exist.yaml")

	for _, tc := range []struct {
		flag     string
		value    string
		wantCode int
	}{
		{"--config", missingConfigPath, 2},
		{"--env", "staging", 2},
		{"--proxy", "file:" + filepath.Join(dir, "capture.jsonl"), 0},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			deps := Deps{
				Probe:   device.NewStaticProbe(nil),
				Factory: device.NewMockDriverFactory(),
				Stdout:  &stdout,
				Stderr:  &stderr,
			}
			code := RunScript(context.Background(), deps, map[string]FlagValue{
				"--file": scriptPath, tc.flag: tc.value,
			})
			if strings.Contains(stderr.String(), "is not supported yet in this build") {
				t.Errorf("stderr still rejects %s as unsupported: %q", tc.flag, stderr.String())
			}
			if code != tc.wantCode {
				t.Errorf("exit code: got %d, want %d; stderr: %s", code, tc.wantCode, stderr.String())
			}
		})
	}
}

// TestRunScriptSmokePassing runs a trivial passing script end to end
// through an injected device.StaticProbe and device.MockDriverFactory,
// with no adb, xcrun, or network access, and asserts the rendered
// text output and exit code.
func TestRunScriptSmokePassing(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	clock := orchestra.NewFakeClock(0)
	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactoryWithDeps(device.MockDriverFactoryDeps{Clock: clock}),
		Clock:   clock,
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "PASS: Untitled script") {
		t.Errorf("stdout missing PASS line: %q", out)
	}
	if !strings.Contains(out, "✓ [1] sleep") {
		t.Errorf("stdout missing sleep step line: %q", out)
	}
}

// TestRunScriptSmokeJSON runs the same trivial script with --json and
// asserts the output is the single pretty-printed JSON blob TS emits,
// not one line per step.
func TestRunScriptSmokeJSON(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--json": true,
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected a multi-line pretty-printed blob, got %d line(s): %q", len(lines), stdout.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded["ok"] != true {
		t.Errorf("ok: got %#v, want true", decoded["ok"])
	}
	if decoded["scriptName"] != "Untitled script" {
		t.Errorf("scriptName: got %#v", decoded["scriptName"])
	}
}

func TestParseReportFormatsDedupesPreservingFirstSeenOrder(t *testing.T) {
	got, err := parseReportFormats("json, html,json , html")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"json", "html"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseReportFormatsErrors(t *testing.T) {
	cases := []struct {
		name    string
		value   FlagValue
		wantErr string
	}{
		{"not a string", true, `--report expects a comma-separated list of formats, got "true"`},
		{"empty after trim", "  , ,", "--report requires at least one format: html or json"},
		{"unrecognized format", "html,xml", `--report format "xml" is not recognized: use html, json, or both, separated by a comma`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseReportFormats(tc.value)
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("got %v, want error %q", err, tc.wantErr)
			}
		})
	}
}

func TestResolveReportOptionsNoFlagsNoConfigWritesFolderWithoutReport(t *testing.T) {
	storageRoot := t.TempDir()
	t.Setenv(storageroot.EnvVar, storageRoot)

	got, err := resolveReportOptions(map[string]FlagValue{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Formats) != 0 {
		t.Errorf("Formats = %v, want none", got.Formats)
	}
	if got.Root != storageRoot {
		t.Errorf("Root = %q, want the shared storage root %q", got.Root, storageRoot)
	}
}

// absPath returns the absolute form of a relative path against the
// working directory, which is how a storage root is resolved.
func absPath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestResolveReportOptionsReportDirOnlyDefaultsToHTML(t *testing.T) {
	got, err := resolveReportOptions(map[string]FlagValue{"--report-dir": "out"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Formats) != 1 || got.Formats[0] != report.FormatHTML || got.Root != absPath(t, "out") {
		t.Errorf("got %#v, want HTML formats and the absolute root of out", got)
	}
}

func TestResolveReportOptionsReportFlagWithoutDirUsesStorageRoot(t *testing.T) {
	storageRoot := t.TempDir()
	t.Setenv(storageroot.EnvVar, storageRoot)

	got, err := resolveReportOptions(map[string]FlagValue{"--report": "json"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Formats) != 1 || got.Formats[0] != report.FormatJSON || got.Root != storageRoot {
		t.Errorf("got %#v, want json formats and root %q", got, storageRoot)
	}
}

func TestResolveReportOptionsUsesConfigReportBlockWhenCliOmitsFlags(t *testing.T) {
	configReport := &config.Report{Formats: []string{"json"}, OutDir: "from-config"}
	got, err := resolveReportOptions(map[string]FlagValue{}, configReport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Formats) != 1 || got.Formats[0] != "json" || got.Root != absPath(t, "from-config") {
		t.Errorf("got %#v, want a report built from configReport", got)
	}
}

func TestResolveReportOptionsReportDirFlagOverridesConfigOutDir(t *testing.T) {
	configReport := &config.Report{Formats: []string{"json"}, OutDir: "from-config"}
	got, err := resolveReportOptions(map[string]FlagValue{"--report-dir": "from-cli"}, configReport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Root != absPath(t, "from-cli") {
		t.Errorf("got %#v, want Root overridden to the absolute path of from-cli", got)
	}
	if len(got.Formats) != 1 || got.Formats[0] != "json" {
		t.Errorf("got %#v, want Formats kept from configReport since --report was not given", got)
	}
}

func TestResolvePreconditionsOverridePrecedence(t *testing.T) {
	cases := []struct {
		name             string
		cli              runner.PreconditionsOverride
		scenarioOverride runner.PreconditionsOverride
		configOverride   runner.PreconditionsOverride
		want             runner.PreconditionsOverride
	}{
		{"none set", runner.PreconditionsNoOverride, runner.PreconditionsNoOverride, runner.PreconditionsNoOverride, runner.PreconditionsNoOverride},
		{"config only", runner.PreconditionsNoOverride, runner.PreconditionsNoOverride, runner.PreconditionsForce, runner.PreconditionsForce},
		{"scenario wins over config", runner.PreconditionsNoOverride, runner.PreconditionsSkip, runner.PreconditionsForce, runner.PreconditionsSkip},
		{"cli wins over scenario and config", runner.PreconditionsForce, runner.PreconditionsSkip, runner.PreconditionsSkip, runner.PreconditionsForce},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePreconditionsOverride(tc.cli, tc.scenarioOverride, tc.configOverride); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePlatformDeviceProxyPrecedence(t *testing.T) {
	deviceID := "config-device"
	proxy := "file:config-capture.jsonl"
	env := &config.ResolvedEnvironment{Platform: "ios", DeviceID: &deviceID, Proxy: &proxy}

	if got := resolvePlatform(map[string]FlagValue{}, nil); got != "android" {
		t.Errorf("resolvePlatform with no flag and no env: got %q, want the default \"android\"", got)
	}
	if got := resolvePlatform(map[string]FlagValue{}, env); got != "ios" {
		t.Errorf("resolvePlatform with no flag: got %q, want the config environment's %q", got, "ios")
	}
	if got := resolvePlatform(map[string]FlagValue{"--platform": "android"}, env); got != "android" {
		t.Errorf("resolvePlatform: got %q, want the CLI flag to win over the config environment", got)
	}

	if got := resolveDeviceID(map[string]FlagValue{}, nil); got != "" {
		t.Errorf("resolveDeviceID with no flag and no env: got %q, want empty", got)
	}
	if got := resolveDeviceID(map[string]FlagValue{}, env); got != deviceID {
		t.Errorf("resolveDeviceID with no flag: got %q, want the config environment's %q", got, deviceID)
	}
	if got := resolveDeviceID(map[string]FlagValue{"--device": "cli-device"}, env); got != "cli-device" {
		t.Errorf("resolveDeviceID: got %q, want the CLI flag to win over the config environment", got)
	}

	if got := resolveProxyFlag(map[string]FlagValue{}, nil); got != "" {
		t.Errorf("resolveProxyFlag with no flag and no env: got %q, want empty", got)
	}
	if got := resolveProxyFlag(map[string]FlagValue{}, env); got != proxy {
		t.Errorf("resolveProxyFlag with no flag: got %q, want the config environment's %q", got, proxy)
	}
	if got := resolveProxyFlag(map[string]FlagValue{"--proxy": "file:cli-capture.jsonl"}, env); got != "file:cli-capture.jsonl" {
		t.Errorf("resolveProxyFlag: got %q, want the CLI flag to win over the config environment", got)
	}
}

func TestParseCaptureFlagShapes(t *testing.T) {
	if got := parseCaptureFlag(""); got != nil {
		t.Errorf("parseCaptureFlag(\"\") = %#v, want nil", got)
	}
	if got := parseCaptureFlag("/tmp/capture.jsonl"); got == nil || got.Type != "file" || got.Path != "/tmp/capture.jsonl" {
		t.Errorf("parseCaptureFlag(bare path) = %#v, want type \"file\"", got)
	}
	if got := parseCaptureFlag("mitmproxy:/tmp/capture.jsonl"); got == nil || got.Type != "mitmproxy" || got.Path != "/tmp/capture.jsonl" {
		t.Errorf("parseCaptureFlag(type:path) = %#v", got)
	}
}

func TestRunScriptProxyFlagBuildsCaptureAndAbsentProxyKeepsNoCapture(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, flags := range []map[string]FlagValue{
		{"--file": scriptPath},
		{"--file": scriptPath, "--proxy": "file:" + filepath.Join(dir, "capture.jsonl")},
	} {
		var stdout, stderr bytes.Buffer
		deps := Deps{
			Probe:   device.NewStaticProbe(nil),
			Factory: device.NewMockDriverFactory(),
			Stdout:  &stdout,
			Stderr:  &stderr,
		}
		code := RunScript(context.Background(), deps, flags)
		if code != 0 {
			t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
		}
	}
}

// proxyRequiredScriptFixture writes a script declaring `proxy:
// required` with a handful of steps, none of which the runner ever
// reaches when the proxy check refuses the run first.
func proxyRequiredScriptFixture(t *testing.T, dir string) string {
	t.Helper()
	scriptPath := filepath.Join(dir, "flow.yml")
	content := "appId: com.test\nproxy: required\n---\n" +
		"- sleep: 1\n- sleep: 1\n- sleep: 1\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// TestRunScriptProxyRequiredWithNoProxyReportsStructuredAbortText runs
// a `proxy: required` script with no --proxy flag and asserts the
// text output names the diagnostic code, the message, and the
// remedy, and that no step ran.
func TestRunScriptProxyRequiredWithNoProxyReportsStructuredAbortText(t *testing.T) {
	dir := t.TempDir()
	scriptPath := proxyRequiredScriptFixture(t, dir)

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath})
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1; stdout: %s", code, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "0/3 steps passed") {
		t.Errorf("stdout: got %q, want it to report zero of three steps passed", out)
	}
	if !strings.Contains(out, "Aborted [PROXY_REQUIRED]: This script needs network capture, but no proxy is configured.") {
		t.Errorf("stdout missing the structured abort line: %q", out)
	}
	if !strings.Contains(out, "--proxy") || !strings.Contains(out, "run_script") {
		t.Errorf("stdout missing a remedy naming both the --proxy flag and the run_script tool: %q", out)
	}
}

// TestRunScriptProxyRequiredWithNoProxyReportsStructuredAbortJSON runs
// the same script with --json and asserts the decoded result carries
// a top level abort object with the same code, message, and remedy,
// alongside zero executed steps.
func TestRunScriptProxyRequiredWithNoProxyReportsStructuredAbortJSON(t *testing.T) {
	dir := t.TempDir()
	scriptPath := proxyRequiredScriptFixture(t, dir)

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scriptPath, "--json": true})
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1; stdout: %s", code, stdout.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded["ok"] != false {
		t.Errorf("ok: got %#v, want false", decoded["ok"])
	}
	if steps, ok := decoded["steps"].([]any); !ok || len(steps) != 0 {
		t.Errorf("steps: got %#v, want an empty array", decoded["steps"])
	}
	if decoded["totalSteps"] != float64(3) || decoded["passedSteps"] != float64(0) {
		t.Errorf("totalSteps/passedSteps: got %#v/%#v, want 3/0", decoded["totalSteps"], decoded["passedSteps"])
	}
	abort, ok := decoded["abort"].(map[string]any)
	if !ok {
		t.Fatalf("abort missing or wrong shape: %#v", decoded["abort"])
	}
	if abort["reason"] != string(runner.RunAbortProxyRequired) {
		t.Errorf("abort.reason: got %#v, want %q", abort["reason"], runner.RunAbortProxyRequired)
	}
	if abort["code"] != "PROXY_REQUIRED" {
		t.Errorf("abort.code: got %#v, want %q", abort["code"], "PROXY_REQUIRED")
	}
	if abort["message"] != "This script needs network capture, but no proxy is configured." {
		t.Errorf("abort.message: got %#v", abort["message"])
	}
	remedy, _ := abort["remedy"].(string)
	if !strings.Contains(remedy, "--proxy") || !strings.Contains(remedy, "run_script") {
		t.Errorf("abort.remedy: got %#v, want it to name both the --proxy flag and the run_script tool", abort["remedy"])
	}
}

// atomyxConfigFixture renders a minimal but complete atomyx.config.yaml
// selecting a single "staging" environment: platform ios, a device id,
// a proxy target, and one config-declared variable, so a test can
// assert every field a config.ResolvedEnvironment threads into a run.
func atomyxConfigFixture(t *testing.T, reportOutDir string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "atomyx.config.yaml")
	content := "project: Demo\n" +
		"apps:\n" +
		"  - id: main\n" +
		"    platform: ios\n" +
		"    appId: com.example.app\n" +
		"environments:\n" +
		"  staging:\n" +
		"    app: main\n" +
		"    device: config-device-1\n" +
		"    vars:\n" +
		"      GREETING: from-config\n" +
		"preconditions: skip\n"
	if reportOutDir != "" {
		content += "report:\n" +
			"  formats: [json]\n" +
			"  outDir: " + reportOutDir + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunScriptConfigEnvThreadsPlatformDeviceVars runs a script that
// asserts on a variable the selected config environment declares,
// with no --platform given, confirming the config environment's own
// platform passes the platform validation (an invalid platform value
// would exit 2 before the script ever ran) and its vars reach the
// script through buildExternalEnv.
func TestRunScriptConfigEnvThreadsPlatformDeviceVars(t *testing.T) {
	configPath := atomyxConfigFixture(t, "")
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "flow.yml")
	scriptContent := "- assertExpr: { expr: 'GREETING == \"from-config\"' }\n"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   configEnvDeviceProbe(),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--config": configPath, "--env": "staging",
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Errorf("stdout missing PASS: %q", stdout.String())
	}
}

// TestRunScriptCliVarOverridesConfigEnvVar confirms --var wins over
// the same key declared in the selected config environment's vars,
// matching buildExternalEnv's own precedence.
func TestRunScriptCliVarOverridesConfigEnvVar(t *testing.T) {
	configPath := atomyxConfigFixture(t, "")
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "flow.yml")
	scriptContent := "- assertExpr: { expr: 'GREETING == \"from-cli\"' }\n"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   configEnvDeviceProbe(),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--config": configPath, "--env": "staging",
		"--var": []string{"GREETING=from-cli"},
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
}

// TestRunScriptConfigReportBlockProducesReportWithNoReportFlag confirms
// a config-declared report block alone (no --report, no --report-dir)
// produces a report bundle, and that its metadata names the selected
// config environment.
func TestRunScriptConfigReportBlockProducesReportWithNoReportFlag(t *testing.T) {
	reportDir := t.TempDir()
	configPath := atomyxConfigFixture(t, reportDir)
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   configEnvDeviceProbe(),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--config": configPath, "--env": "staging",
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Report: ") {
		t.Fatalf("stderr missing the report bundle line: %q", stderr.String())
	}

	bundleJSON := findReportJSON(t, reportDir)
	var decoded map[string]any
	if err := json.Unmarshal(bundleJSON, &decoded); err != nil {
		t.Fatalf("invalid report.json: %v", err)
	}
	metadata, ok := decoded["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing or wrong shape: %#v", decoded["metadata"])
	}
	if metadata["env"] != "staging" {
		t.Errorf("metadata.env: got %#v, want \"staging\"", metadata["env"])
	}
}

// TestWriteReport_DeviceLostAbort_CarriedIntoReportMetadata guards
// against a genuine device-lost abort being computed by the runner
// but dropped before it reaches the report bundle: it fails a step
// against a live (uncanceled) context, with the mock driver's Ping
// staged to report the agent unreachable, and asserts report.json's
// metadata carries the same abort reason and code
// runner.ScriptResult.Abort produced, not just an internal struct
// field the CLI never renders.
func TestWriteReport_DeviceLostAbort_CarriedIntoReportMetadata(t *testing.T) {
	drv := mock.New()
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})
	drv.FailNextPingForContract(diag.New(
		diag.CodeAgentUnreachable,
		"could not reach the agent on device mock-1",
		"Confirm the agent is running on the device, then try again.",
		nil,
	))

	clock := orchestra.NewFakeClock(0)
	orch := orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: clock})
	sr := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra: orch,
		Clock:     clock,
		Logger:    logging.NoopLogger{},
		Commands:  runner.DefaultCommands(),
	})

	def, err := script.ParseScript("appId: com.test\n---\n- assertVisible: \"Does not exist\"\n", nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	result, err := sr.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Abort == nil {
		t.Fatal("result.Abort is nil, want a device-lost abort to assert against")
	}

	runDir := t.TempDir()
	if _, err := writeReport("run-abort-1", result, time.Now(), time.Now(), "android", "mock-1", nil, nil, report.ReportOptions{
		Formats:      []string{report.FormatJSON},
		ReportDir:    runfolder.ReportDir(runDir),
		ArtifactsDir: runfolder.ArtifactsDir(runDir),
	}); err != nil {
		t.Fatalf("writeReport: %v", err)
	}

	bundleJSON, err := os.ReadFile(filepath.Join(runfolder.ReportDir(runDir), "report.json"))
	if err != nil {
		t.Fatalf("reading report.json: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bundleJSON, &decoded); err != nil {
		t.Fatalf("invalid report.json: %v", err)
	}
	metadata, ok := decoded["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing or wrong shape: %#v", decoded["metadata"])
	}
	abort, ok := metadata["abort"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.abort missing or wrong shape: %#v", metadata["abort"])
	}
	if abort["reason"] != string(runner.RunAbortDeviceLost) {
		t.Errorf("metadata.abort.reason = %#v, want %q", abort["reason"], runner.RunAbortDeviceLost)
	}
	if abort["code"] != diag.CodeAgentUnreachable {
		t.Errorf("metadata.abort.code = %#v, want %q", abort["code"], diag.CodeAgentUnreachable)
	}
}

// TestRunScriptDispatchesScenarioInsteadOfScript confirms a --file
// whose YAML matches the scenario shape (a `scripts` list) runs
// through the ScenarioRunner path instead of the single-script path:
// both child scripts run, resolved relative to the scenario file's
// own directory, and the printed result carries the scenario
// vocabulary ("scenario", "scripts passed") rather than the script
// one ("steps passed").
func TestRunScriptDispatchesScenarioInsteadOfScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child-a.yml"), []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child-b.yml"), []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join(dir, "scenario.yml")
	scenarioContent := "name: Smoke scenario\nscripts:\n  - child-a.yml\n  - child-b.yml\n"
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{"--file": scenarioPath})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "scenario \"Smoke scenario\"") {
		t.Errorf("stdout missing scenario heading: %q", out)
	}
	if !strings.Contains(out, "2/2 scripts passed") {
		t.Errorf("stdout missing scripts-passed line: %q", out)
	}
	if !strings.Contains(out, "child-a.yml") || !strings.Contains(out, "child-b.yml") {
		t.Errorf("stdout missing a child script line: %q", out)
	}
}

// TestRunScriptScenarioJSONOutputShape confirms --json on a scenario
// run emits the scenario JSON shape (scenarioName, scripts array),
// with each child's screenshot data reduced to size metadata exactly
// like the single-script --json path.
func TestRunScriptScenarioJSONOutputShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.yml"), []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join(dir, "scenario.yml")
	if err := os.WriteFile(scenarioPath, []byte("name: Smoke\nscripts:\n  - child.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scenarioPath, "--json": true,
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded["scenarioName"] != "Smoke" {
		t.Errorf("scenarioName: got %#v", decoded["scenarioName"])
	}
	scripts, ok := decoded["scripts"].([]any)
	if !ok || len(scripts) != 1 {
		t.Fatalf("scripts missing or wrong shape: %#v", decoded["scripts"])
	}
}

// TestRunScriptScenarioReportProducesScenarioKindBundle confirms
// --report on a scenario run writes a report.json whose top-level
// "kind" is "scenario", not "script", matching how the TypeScript CLI
// switches ReportInput kind on the same branch.
func TestRunScriptScenarioReportProducesScenarioKindBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child.yml"), []byte("- sleep: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join(dir, "scenario.yml")
	if err := os.WriteFile(scenarioPath, []byte("name: Smoke\nscripts:\n  - child.yml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reportDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Factory: device.NewMockDriverFactory(),
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scenarioPath, "--report": "json", "--report-dir": reportDir,
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	bundleJSON := findReportJSON(t, reportDir)
	var decoded map[string]any
	if err := json.Unmarshal(bundleJSON, &decoded); err != nil {
		t.Fatalf("invalid report.json: %v", err)
	}
	if decoded["kind"] != "scenario" {
		t.Errorf("kind: got %#v, want \"scenario\"", decoded["kind"])
	}
}

// singleRunDir returns the run folder the one run under root wrote.
func singleRunDir(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(runfolder.VersionedRunsDir(root))
	if err != nil {
		t.Fatalf("reading the versioned runs directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("root has %d run folders, want exactly 1: %v", len(entries), entries)
	}
	return runfolder.RunDir(root, entries[0].Name())
}

// findReportJSON returns the raw bytes of the report.json the single
// run under root wrote.
func findReportJSON(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runfolder.ReportDir(singleRunDir(t, root)), "report.json"))
	if err != nil {
		t.Fatalf("reading report.json: %v", err)
	}
	return data
}

// findReportHTML returns the raw bytes of the report.html the single
// run under root wrote.
func findReportHTML(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runfolder.ReportDir(singleRunDir(t, root)), "report.html"))
	if err != nil {
		t.Fatalf("reading report.html: %v", err)
	}
	return data
}

// TestRunScriptSecretResolvedFromEnvRefNeverPersistedInReport runs a
// script that types a value resolved from a config environment's
// `${ENV:NAME}` reference, with both report formats enabled, and
// asserts the secret's literal value appears in neither report.json
// nor report.html's rendered bytes.
func TestRunScriptSecretResolvedFromEnvRefNeverPersistedInReport(t *testing.T) {
	const secretEnvVar = "ATOMYX_TEST_SECRET"
	const secretValue = "very-secret-token-9f3a"
	t.Setenv(secretEnvVar, secretValue)

	reportDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "atomyx.config.yaml")
	configContent := "project: Demo\n" +
		"apps:\n" +
		"  - id: main\n" +
		"    platform: ios\n" +
		"    appId: com.example.app\n" +
		"environments:\n" +
		"  staging:\n" +
		"    app: main\n" +
		"    vars:\n" +
		"      SECRET: \"${ENV:" + secretEnvVar + "}\"\n" +
		"report:\n" +
		"  formats: [html, json]\n" +
		"  outDir: " + reportDir + "\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "flow.yml")
	if err := os.WriteFile(scriptPath, []byte("- type: '${SECRET}'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Deps{
		Probe:   device.NewStaticProbe(nil),
		Runners: servedSimulatorRunner{},
		Factory: stagedMockFactory{tree: focusedInputTree()},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}
	code := RunScript(context.Background(), deps, map[string]FlagValue{
		"--file": scriptPath, "--config": configPath, "--env": "staging",
	})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	reportJSON := findReportJSON(t, reportDir)
	if bytes.Contains(reportJSON, []byte(secretValue)) {
		t.Fatalf("report.json contains the secret literal: %s", reportJSON)
	}
	reportHTML := findReportHTML(t, reportDir)
	if bytes.Contains(reportHTML, []byte(secretValue)) {
		t.Fatalf("report.html contains the secret literal: %s", reportHTML)
	}
}

func TestFormatReportTimestampMatchesJSToISOString(t *testing.T) {
	loc := time.FixedZone("UTC+7", 7*60*60)
	at := time.Date(2026, 9, 14, 10, 59, 31, 64_000_000, loc)

	got := formatReportTimestamp(at)
	want := "2026-09-14T03:59:31.064Z"
	if got != want {
		t.Errorf("got %q, want %q (UTC, millisecond precision, trailing Z)", got, want)
	}
}

// emptyArtifacts and fakeArtifacts are minimal runner.ScriptArtifacts
// implementations for rendering tests that don't need a real script
// run.
type emptyArtifacts struct{}

func (emptyArtifacts) AddScreenshot(string, []byte, string) {}
func (emptyArtifacts) Screenshots() []runner.Screenshot     { return nil }

type fakeArtifacts struct{ shots []runner.Screenshot }

func (fakeArtifacts) AddScreenshot(string, []byte, string) {}
func (a fakeArtifacts) Screenshots() []runner.Screenshot {
	return a.shots
}
