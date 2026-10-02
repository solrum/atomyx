package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
)

// Flag, project config, and precedence resolution: turning the run
// command's flags and project config into the concrete values the run
// flow acts on.

// resolveEnvironment loads the project config, when one exists, and
// resolves it down to the environment this run should use:
// config.Load runs first (bypassing discovery for an explicit
// --config path), then config.SelectEnvironment runs against --env.
// The returned
// *config.ResolvedEnvironment is nil when no environment applies, in
// which case platform, device, proxy, and variables come from CLI
// flags alone; the returned *config.LoadedConfig is nil when no
// config file was found at all, independent of whether an environment
// resolved, so callers that only need project-level settings (like
// the report block or the preconditions default) read it directly.
func resolveEnvironment(flags map[string]FlagValue) (*config.LoadedConfig, *config.ResolvedEnvironment, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	configPath, _ := flagString(flags, "--config")

	loaded, err := config.Load(config.LoadOptions{Cwd: cwd, ConfigPath: configPath})
	if err != nil {
		return nil, nil, err
	}

	envName, hasEnv := flagString(flags, "--env")
	resolvedEnvironment, err := config.SelectEnvironment(loaded, envName, hasEnv)
	if err != nil {
		return nil, nil, err
	}
	return loaded, resolvedEnvironment, nil
}

// configPreconditionsOverride reads the project config's own
// preconditions default, or runner.PreconditionsNoOverride when no
// config was found or it declares none.
func configPreconditionsOverride(loaded *config.LoadedConfig) runner.PreconditionsOverride {
	if loaded == nil || loaded.Config.Preconditions == nil {
		return runner.PreconditionsNoOverride
	}
	return runner.PreconditionsOverride(*loaded.Config.Preconditions)
}

// resolvePreconditionsOverride resolves the run-level precondition
// override from every source that can set one, highest precedence
// first: the CLI flag, the scenario's own `preconditions` field
// (runner.PreconditionsNoOverride for a standalone script, which has
// no such field), then the project config's `preconditions` field.
// runner.PreconditionsNoOverride when none of the three sets a value,
// which leaves every precondition's own policy in effect.
func resolvePreconditionsOverride(cli, scenarioOverride, configOverride runner.PreconditionsOverride) runner.PreconditionsOverride {
	if cli != runner.PreconditionsNoOverride {
		return cli
	}
	if scenarioOverride != runner.PreconditionsNoOverride {
		return scenarioOverride
	}
	return configOverride
}

// resolvePlatform picks the platform a run targets, highest
// precedence first: the --platform flag, the selected config
// environment's own platform, then "android".
func resolvePlatform(flags map[string]FlagValue, env *config.ResolvedEnvironment) string {
	if v, ok := flagString(flags, "--platform"); ok {
		return v
	}
	if env != nil {
		return env.Platform
	}
	return "android"
}

// resolveDeviceID picks the device a run targets, highest precedence
// first: the --device flag, then the selected config environment's
// own device. Empty when neither sets one, which resolveDescriptor maps
// to its own platform-specific default.
func resolveDeviceID(flags map[string]FlagValue, env *config.ResolvedEnvironment) string {
	if v, ok := flagString(flags, "--device"); ok {
		return v
	}
	if env != nil && env.DeviceID != nil {
		return *env.DeviceID
	}
	return ""
}

// resolveProxyFlag picks the raw --proxy value a run passes to
// parseCaptureFlag, highest precedence first: the --proxy flag, then
// the selected config environment's own proxy. Empty when neither
// sets one, which parseCaptureFlag maps to no capture.
func resolveProxyFlag(flags map[string]FlagValue, env *config.ResolvedEnvironment) string {
	if v, ok := flagString(flags, "--proxy"); ok {
		return v
	}
	if env != nil && env.Proxy != nil {
		return *env.Proxy
	}
	return ""
}

// parseCaptureFlag parses the --proxy flag's "type:path" shape into a
// netcapture.Config via netcapture.ParseConfigString, the same parser
// the MCP server's run_script tool uses for its own "proxy" argument,
// so the two surfaces accept the same syntax.
func parseCaptureFlag(flag string) *netcapture.Config {
	return netcapture.ParseConfigString(flag)
}

// resolveDescriptor builds the device.Descriptor a command targets
// through device.ResolveDescriptor, so the device kind comes from the
// device's identity as the probe sees it. An iOS --device that no
// scan finds is reported as DEVICE_NOT_FOUND with a remedy.
func resolveDescriptor(ctx context.Context, probe device.Probe, runners device.RunnerIdentifier, platform, deviceID string) (device.Descriptor, error) {
	desc, err := device.ResolveDescriptor(ctx, probe, runners, device.Platform(platform), deviceID)
	var notFound *device.DeviceNotFoundError
	if errors.As(err, &notFound) && notFound.ID == "" {
		return device.Descriptor{}, diag.New(
			diag.CodeDeviceNotFound,
			notFound.Error(),
			"Serve the runner on the target device (make serve in platforms/ios-agent), or pass --device with an id from `atomyx list-devices`.",
			err,
		)
	}
	if errors.As(err, &notFound) {
		return device.Descriptor{}, diag.New(
			diag.CodeDeviceNotFound,
			notFound.Error(),
			"Run `atomyx list-devices` to see the reachable iOS simulators and physical devices, then pass one of those ids with --device. A physical iPhone must be connected over USB, unlocked, and trusted.",
			err,
		)
	}
	return desc, err
}

// resolveCliPreconditionsFlag combines --preconditions and its
// --skip-preconditions shorthand into a single runner.PreconditionsOverride.
// It returns an error when --preconditions carries a value other than
// "skip" or "force", or when the shorthand is combined with a
// --preconditions value that isn't "skip".
func resolveCliPreconditionsFlag(flags map[string]FlagValue) (runner.PreconditionsOverride, error) {
	shorthand := flagBool(flags, "--skip-preconditions")
	value, hasValue := flagString(flags, "--preconditions")

	if hasValue && value != "skip" && value != "force" {
		return "", fmt.Errorf("--preconditions expects \"skip\" or \"force\", got %q", value)
	}
	if shorthand && hasValue && value != "skip" {
		return "", fmt.Errorf("--skip-preconditions conflicts with --preconditions %q", value)
	}
	if shorthand {
		return runner.PreconditionsSkip, nil
	}
	if value == "skip" {
		return runner.PreconditionsSkip, nil
	}
	if value == "force" {
		return runner.PreconditionsForce, nil
	}
	return runner.PreconditionsNoOverride, nil
}

// buildExternalEnv builds the caller-supplied variable set for
// script.ParseScript. Its precedence, highest first: --var,
// --var-file, the selected config environment's own `vars` (plus its
// app id, exposed as "appId"), then the script's own inline env block
// (applied inside script.ParseScript itself, not here).
func buildExternalEnv(flags map[string]FlagValue, env *config.ResolvedEnvironment) (map[string]string, error) {
	out := map[string]string{}

	if env != nil {
		for k, v := range env.Vars {
			out[k] = v
		}
		out["appId"] = env.AppID
	}

	if varFile, ok := flagString(flags, "--var-file"); ok {
		fromFile, err := readVarFile(varFile)
		if err != nil {
			return nil, err
		}
		for k, v := range fromFile {
			out[k] = v
		}
	}

	for _, pair := range flagStrings(flags, "--var") {
		eqIdx := strings.IndexByte(pair, '=')
		if eqIdx <= 0 {
			return nil, fmt.Errorf("--var expects \"KEY=VALUE\", got %q", pair)
		}
		out[pair[:eqIdx]] = pair[eqIdx+1:]
	}

	return out, nil
}

func readVarFile(path string) (map[string]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("--var-file not found: %s", absPath)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("--var-file is not valid JSON: %s", absPath)
	}
	out := make(map[string]string, len(parsed))
	for key, value := range parsed {
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("--var-file value for %q must be a string: %s", key, absPath)
		}
		out[key] = s
	}
	return out, nil
}

// resolveReportOptions combines the --report and --report-dir flags
// with an optional project-config report block into a
// report.ReportOptions. Reporting is opt-in: nil when neither the CLI
// flags nor configReport request it, and an error when reporting is
// requested but no output directory is given. The CLI flags win over
// configReport field by field, so --report-dir alone can redirect a
// config-declared report without also having to repeat --report.
func resolveReportOptions(flags map[string]FlagValue, configReport *report.ReportOptions) (*report.ReportOptions, error) {
	rawFormats, hasFormats := flags["--report"]
	rawOutDir, hasOutDir := flags["--report-dir"]
	cliRequestsReport := hasFormats || hasOutDir

	if !cliRequestsReport && configReport == nil {
		return nil, nil
	}

	var formats []string
	switch {
	case hasFormats:
		parsed, err := parseReportFormats(rawFormats)
		if err != nil {
			return nil, err
		}
		formats = parsed
	case configReport != nil:
		formats = configReport.Formats
	default:
		formats = []string{report.FormatHTML}
	}

	var outDir string
	if hasOutDir {
		outDir, _ = rawOutDir.(string)
	} else if configReport != nil {
		outDir = configReport.OutDir
	}
	if outDir == "" {
		return nil, fmt.Errorf(
			"reporting is enabled, but no output directory is set. Pass --report-dir <path>, or set report.outDir in the project config",
		)
	}

	return &report.ReportOptions{Formats: formats, OutDir: outDir}, nil
}

// parseReportFormats parses the raw --report flag value (e.g.
// "html,json") into a deduped, first-seen-order list of report
// formats, rejecting anything other than "html" or "json". value is
// untyped because a flag map entry can hold any FlagValue; only the
// string case is a valid --report value.
func parseReportFormats(value FlagValue) ([]string, error) {
	s, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("--report expects a comma-separated list of formats, got %q", fmt.Sprint(value))
	}

	parts := strings.Split(s, ",")
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		trimmed = append(trimmed, p)
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("--report requires at least one format: html or json")
	}

	seen := make(map[string]bool, len(trimmed))
	formats := make([]string, 0, len(trimmed))
	for _, part := range trimmed {
		if part != report.FormatHTML && part != report.FormatJSON {
			return nil, fmt.Errorf("--report format %q is not recognized: use html, json, or both, separated by a comma", part)
		}
		if !seen[part] {
			seen[part] = true
			formats = append(formats, part)
		}
	}
	return formats, nil
}

func flagString(flags map[string]FlagValue, name string) (string, bool) {
	v, ok := flags[name]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func flagBool(flags map[string]FlagValue, name string) bool {
	v, ok := flags[name].(bool)
	return ok && v
}

func flagStrings(flags map[string]FlagValue, name string) []string {
	v, _ := flags[name].([]string)
	return v
}
