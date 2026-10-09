package cli

import (
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/config"
)

func mustParseArgv(t *testing.T, args []string) ParsedArgv {
	t.Helper()
	argv, err := ParseArgv(args)
	if err != nil {
		t.Fatalf("ParseArgv(%v): unexpected error: %v", args, err)
	}
	return argv
}

func TestParseArgvCommands(t *testing.T) {
	if got := mustParseArgv(t, nil).Command; got != CommandHelp {
		t.Errorf("empty args: got command %q, want help", got)
	}
	if got := mustParseArgv(t, []string{"--help"}).Command; got != CommandHelp {
		t.Errorf("--help: got command %q, want help", got)
	}
	if got := mustParseArgv(t, []string{"-h"}).Command; got != CommandHelp {
		t.Errorf("-h: got command %q, want help", got)
	}
	if got := mustParseArgv(t, []string{"--version"}).Command; got != CommandVersion {
		t.Errorf("--version: got command %q, want version", got)
	}
	if got := mustParseArgv(t, []string{"-v"}).Command; got != CommandVersion {
		t.Errorf("-v: got command %q, want version", got)
	}

	result := mustParseArgv(t, []string{"list-devices"})
	if result.Command != CommandListDevices {
		t.Errorf("list-devices: got command %q", result.Command)
	}
	if _, ok := result.Flags["--json"]; ok {
		t.Errorf("list-devices with no --json: flag present")
	}

	if got := mustParseArgv(t, []string{"run", "--file", "test.yml"}).Command; got != CommandRun {
		t.Errorf("run: got command %q, want run", got)
	}

	result = mustParseArgv(t, []string{"doctor", "--platform", "ios", "--device", "UDID"})
	if result.Command != CommandDoctor {
		t.Errorf("doctor: got command %q, want doctor", result.Command)
	}
	if v, _ := result.Flags["--platform"].(string); v != "ios" {
		t.Errorf("doctor --platform: got %v, want ios", result.Flags["--platform"])
	}
}

func TestParseArgvUnknownCommand(t *testing.T) {
	if _, err := ParseArgv([]string{"nonsense"}); !isArgvError(err) {
		t.Errorf("unknown command: got %v, want *ArgvError", err)
	}
}

func TestParseArgvUnknownCommandMentionsMCP(t *testing.T) {
	_, err := ParseArgv([]string{"mcp", "--platform", "android"})
	if !isArgvError(err) {
		t.Fatalf("got %v, want *ArgvError", err)
	}
	if !containsSubstring(err.Error(), "atomyx-mcp") {
		t.Errorf("error %q does not mention atomyx-mcp", err.Error())
	}
}

func TestParseArgvSuitePositionalAndFlags(t *testing.T) {
	result := mustParseArgv(t, []string{"suite"})
	if result.Command != CommandSuite {
		t.Errorf("suite: got command %q, want suite", result.Command)
	}
	if result.Positional != nil {
		t.Errorf("suite with no positional: got %#v, want nil", result.Positional)
	}

	result = mustParseArgv(t, []string{"suite", "smoke"})
	if result.Positional == nil || *result.Positional != "smoke" {
		t.Errorf("suite smoke: got positional %#v, want \"smoke\"", result.Positional)
	}

	result = mustParseArgv(t, []string{"suite", "--tag", "smoke"})
	if result.Positional != nil {
		t.Errorf("suite --tag smoke: got positional %#v, want nil", result.Positional)
	}
	assertStringSlice(t, result.Flags["--tag"], []string{"smoke"})

	result = mustParseArgv(t, []string{"suite", "smoke", "--glob", "flows/*.yml"})
	if result.Positional == nil || *result.Positional != "smoke" {
		t.Errorf("suite smoke --glob: got positional %#v, want \"smoke\"", result.Positional)
	}
	assertStringSlice(t, result.Flags["--glob"], []string{"flows/*.yml"})
}

func TestParseArgvSuiteRejectsUnknownFlag(t *testing.T) {
	if _, err := ParseArgv([]string{"suite", "--bogus"}); !isArgvError(err) {
		t.Errorf("suite --bogus: got %v, want *ArgvError", err)
	}
}

func TestParseArgvRejectsUnknownPositionalNoise(t *testing.T) {
	if _, err := ParseArgv([]string{"list-devices", "foo"}); !isArgvError(err) {
		t.Errorf("got %v, want *ArgvError", err)
	}
}

func TestParseArgvBooleanFlags(t *testing.T) {
	result := mustParseArgv(t, []string{"list-devices", "--json"})
	if v, ok := result.Flags["--json"].(bool); !ok || !v {
		t.Errorf("--json: got %#v, want true", result.Flags["--json"])
	}

	if _, err := ParseArgv([]string{"version", "--json"}); !isArgvError(err) {
		t.Errorf("version --json: got %v, want *ArgvError", err)
	}
	if _, err := ParseArgv([]string{"list-devices", "--bogus"}); !isArgvError(err) {
		t.Errorf("list-devices --bogus: got %v, want *ArgvError", err)
	}
}

func TestParseArgvValueFlags(t *testing.T) {
	result := mustParseArgv(t, []string{"run", "--file", "test.yml"})
	if result.Flags["--file"] != "test.yml" {
		t.Errorf("--file: got %#v", result.Flags["--file"])
	}

	result = mustParseArgv(t, []string{"run", "--file=test.yml"})
	if result.Flags["--file"] != "test.yml" {
		t.Errorf("--file=value: got %#v", result.Flags["--file"])
	}

	result = mustParseArgv(t, []string{
		"run",
		"--file", "flow.yml",
		"--platform", "ios",
		"--device", "ABC-123",
		"--proxy", "file:/tmp/cap.jsonl",
		"--json",
	})
	want := map[string]FlagValue{
		"--file": "flow.yml", "--platform": "ios", "--device": "ABC-123",
		"--proxy": "file:/tmp/cap.jsonl", "--json": true,
	}
	for k, v := range want {
		if result.Flags[k] != v {
			t.Errorf("flag %s: got %#v, want %#v", k, result.Flags[k], v)
		}
	}

	if _, err := ParseArgv([]string{"run", "--file"}); !containsSubstring(errString(err), "requires a value") {
		t.Errorf("missing value: got %v", err)
	}
	if _, err := ParseArgv([]string{"run", "--file", "--json"}); !containsSubstring(errString(err), "requires a value") {
		t.Errorf("next-flag-as-value: got %v", err)
	}
}

func TestParseArgvRepeatableVar(t *testing.T) {
	result := mustParseArgv(t, []string{"run", "--file", "x.yml", "--var", "FOO=bar"})
	assertStringSlice(t, result.Flags["--var"], []string{"FOO=bar"})

	result = mustParseArgv(t, []string{
		"run", "--file", "x.yml", "--var", "FOO=bar", "--var", "BAZ=qux",
	})
	assertStringSlice(t, result.Flags["--var"], []string{"FOO=bar", "BAZ=qux"})

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--var=FOO=bar=baz"})
	assertStringSlice(t, result.Flags["--var"], []string{"FOO=bar=baz"})

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--var-file", "vars.json"})
	if result.Flags["--var-file"] != "vars.json" {
		t.Errorf("--var-file: got %#v", result.Flags["--var-file"])
	}
}

func TestParseArgvConfigAndReportFlags(t *testing.T) {
	result := mustParseArgv(t, []string{"run", "--file", "x.yml", "--config", "atomyx.config.yaml"})
	if result.Flags["--config"] != "atomyx.config.yaml" {
		t.Errorf("--config: got %#v", result.Flags["--config"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--env", "staging"})
	if result.Flags["--env"] != "staging" {
		t.Errorf("--env: got %#v", result.Flags["--env"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--report", "html,json"})
	if result.Flags["--report"] != "html,json" {
		t.Errorf("--report: got %#v", result.Flags["--report"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--report-dir", "./reports"})
	if result.Flags["--report-dir"] != "./reports" {
		t.Errorf("--report-dir: got %#v", result.Flags["--report-dir"])
	}
}

func TestParseArgvPreconditionsFlags(t *testing.T) {
	result := mustParseArgv(t, []string{"run", "--file", "x.yml", "--preconditions", "skip"})
	if result.Flags["--preconditions"] != "skip" {
		t.Errorf("--preconditions: got %#v", result.Flags["--preconditions"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--preconditions=force"})
	if result.Flags["--preconditions"] != "force" {
		t.Errorf("--preconditions=value: got %#v", result.Flags["--preconditions"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml", "--skip-preconditions"})
	if v, ok := result.Flags["--skip-preconditions"].(bool); !ok || !v {
		t.Errorf("--skip-preconditions: got %#v", result.Flags["--skip-preconditions"])
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml"})
	if _, ok := result.Flags["--preconditions"]; ok {
		t.Errorf("neither flag: --preconditions present")
	}
	if _, ok := result.Flags["--skip-preconditions"]; ok {
		t.Errorf("neither flag: --skip-preconditions present")
	}
}

func TestParseArgvPerCommandHelp(t *testing.T) {
	result := mustParseArgv(t, []string{"run", "--help"})
	if result.Command != CommandRun || !result.Help {
		t.Errorf("run --help: got %+v", result)
	}

	result = mustParseArgv(t, []string{"run", "-h"})
	if result.Command != CommandRun || !result.Help {
		t.Errorf("run -h: got %+v", result)
	}

	result = mustParseArgv(t, []string{"list-devices", "--help"})
	if result.Command != CommandListDevices || !result.Help {
		t.Errorf("list-devices --help: got %+v", result)
	}

	result = mustParseArgv(t, []string{"run", "--file", "x.yml"})
	if result.Help {
		t.Errorf("run without --help: Help is true")
	}

	// "-h" must trigger help rather than bind as the suite command's
	// positional argument.
	result = mustParseArgv(t, []string{"suite", "-h"})
	if result.Command != CommandSuite || !result.Help {
		t.Errorf("suite -h: got %+v", result)
	}
	if result.Positional != nil {
		t.Errorf("suite -h: got positional %#v, want nil", result.Positional)
	}

	result = mustParseArgv(t, []string{"suite", "--help"})
	if result.Command != CommandSuite || !result.Help {
		t.Errorf("suite --help: got %+v", result)
	}
	if result.Positional != nil {
		t.Errorf("suite --help: got positional %#v, want nil", result.Positional)
	}
}

func isArgvError(err error) bool {
	var argvErr *ArgvError
	return errors.As(err, &argvErr)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

func assertStringSlice(t *testing.T, v FlagValue, want []string) {
	t.Helper()
	got, ok := v.([]string)
	if !ok {
		t.Fatalf("got %#v, want []string", v)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestParseArgvInspectAcceptsItsFlagsAndRejectsOthers(t *testing.T) {
	result := mustParseArgv(t, []string{"inspect", "--platform", "ios", "--device", "UDID", "--no-open"})
	if result.Command != CommandInspect || !isTruthy(result.Flags["--no-open"]) || result.Flags["--device"] != "UDID" {
		t.Fatalf("parsed = %+v", result)
	}
	if _, err := ParseArgv([]string{"inspect", "--file", "x.yml"}); err == nil {
		t.Fatal("inspect accepted --file")
	}
}

func TestParseArgvVideoFlagIsBooleanOnRunAndSuite(t *testing.T) {
	run := mustParseArgv(t, []string{"run", "--file", "x.yml", "--video"})
	if v, ok := run.Flags["--video"].(bool); !ok || !v {
		t.Errorf("run --video: got %#v", run.Flags["--video"])
	}
	suiteArgs := mustParseArgv(t, []string{"suite", "smoke", "--video"})
	if v, ok := suiteArgs.Flags["--video"].(bool); !ok || !v {
		t.Errorf("suite --video: got %#v", suiteArgs.Flags["--video"])
	}
	if _, ok := mustParseArgv(t, []string{"run", "--file", "x.yml"}).Flags["--video"]; ok {
		t.Errorf("run without --video: flag present")
	}
}

func TestVideoEnabledFlagOverridesConfig(t *testing.T) {
	on, off := config.VideoOnFailure, config.VideoOff
	cfg := func(v *string) *config.LoadedConfig {
		return &config.LoadedConfig{Config: config.AtomyxConfig{Video: v}}
	}
	cases := []struct {
		name   string
		flags  map[string]FlagValue
		loaded *config.LoadedConfig
		want   bool
	}{
		{"no flag no config", map[string]FlagValue{}, nil, false},
		{"flag without config", map[string]FlagValue{"--video": true}, nil, true},
		{"flag over off", map[string]FlagValue{"--video": true}, cfg(&off), true},
		{"config onFailure", map[string]FlagValue{}, cfg(&on), true},
		{"config off", map[string]FlagValue{}, cfg(&off), false},
	}
	for _, c := range cases {
		if got := videoEnabled(c.flags, c.loaded); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseArgvRetriesIsASuiteFlagOnly(t *testing.T) {
	result := mustParseArgv(t, []string{"suite", "smoke", "--retries", "2"})
	if result.Flags["--retries"] != "2" {
		t.Errorf("--retries: got %#v", result.Flags["--retries"])
	}
	if _, err := ParseArgv([]string{"run", "--file", "x.yml", "--retries", "2"}); !isArgvError(err) {
		t.Errorf("run accepted --retries: %v", err)
	}
}
