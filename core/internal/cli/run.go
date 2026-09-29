package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
)

// The run command's top-level flow: parse and validate its flags, connect
// the device, execute the script or scenario, then render the result and
// any report.

// RunScript runs the single YML test script named by flags["--file"]
// against a device built through deps.Factory, then renders the
// result to deps.Stdout (or deps.Stderr for a usage error).
//
// Exit codes are part of the command's contract: 0 when the script
// passed, 1 when it ran and failed OR when an unexpected error
// aborted the run before a result was produced (a missing script
// file, a script that fails to parse, a device that could not be
// reached), 2 when the command itself was misused (a missing
// --file, an invalid flag value).
func RunScript(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

	filePath, ok := flagString(flags, "--file")
	if !ok {
		_, _ = io.WriteString(stderr, "error: --file <path> is required for the run command.\n")
		return 2
	}

	loaded, resolvedEnvironment, err := resolveEnvironment(flags)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	cliPreconditionsFlag, err := resolveCliPreconditionsFlag(flags)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}
	fromConfigPreconditions := configPreconditionsOverride(loaded)

	var configReport *report.ReportOptions
	if loaded != nil {
		configReport = loaded.Config.Report
	}
	reportOptions, err := resolveReportOptions(flags, configReport)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	platform := resolvePlatform(flags, resolvedEnvironment)
	if platform != "android" && platform != "ios" {
		_, _ = fmt.Fprintf(stderr, "error: Unknown platform %q. Use --platform android or --platform ios.\n", platform)
		return 2
	}
	deviceID := resolveDeviceID(flags, resolvedEnvironment)
	proxyFlag := resolveProxyFlag(flags, resolvedEnvironment)

	externalEnv, err := buildExternalEnv(flags, resolvedEnvironment)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	jsonOutput := flagBool(flags, "--json")

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}

	// The device connects before the script or scenario is parsed.
	// When both the device is unreachable and the file fails to
	// parse, the device error is the one reported, because parsing
	// never runs until the connection succeeds.
	desc := descriptorFor(platform, deviceID)
	built, err := deps.Factory.Build(ctx, desc)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}
	// Disconnect is best-effort cleanup: by the time this runs, the
	// command has already produced its report and exit code, so a
	// failure to tear down the device connection has nothing left to
	// report to.
	defer func() { _ = built.Driver.Disconnect() }()

	captureConfig := parseCaptureFlag(proxyFlag)
	networkCapture, err := netcapture.New(captureConfig)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}

	var envName *string
	if resolvedEnvironment != nil {
		name := resolvedEnvironment.Name
		envName = &name
	}

	runStartedAt := time.Now()

	// runID is generated once, before the script or scenario runs, so
	// the same value both correlates this run's live step trace and
	// names its report bundle directory, letting an operator go from
	// a logged line straight to the matching report.
	runID := generateRunID(runStartedAt)

	if script.IsScenarioYaml(string(content)) {
		scenarioDef, err := script.ParseScenario(string(content))
		if err != nil {
			return reportUncaught(stderr, deps.Debug, err)
		}

		fromScenarioPreconditions := runner.PreconditionsNoOverride
		if scenarioDef.Preconditions != nil {
			fromScenarioPreconditions = runner.PreconditionsOverride(*scenarioDef.Preconditions)
		}
		preconditionsOverride := resolvePreconditionsOverride(cliPreconditionsFlag, fromScenarioPreconditions, fromConfigPreconditions)

		scenarioRunner := scenario.NewScenarioRunner(scenario.ScenarioRunnerDeps{
			Orchestra:             built.Orchestra,
			Clock:                 deps.clock(),
			Logger:                deps.logger(),
			RunID:                 runID,
			NetworkCapture:        networkCapture,
			Commands:              runner.DefaultCommands(),
			LoadScript:            fsScenarioScriptLoader(filepath.Dir(absPath)),
			ExternalEnv:           externalEnv,
			PreconditionsOverride: preconditionsOverride,
		})
		result, err := scenarioRunner.Run(ctx, scenarioDef)
		finishedAt := time.Now()
		if err != nil {
			return reportUncaught(stderr, deps.Debug, err)
		}

		if jsonOutput {
			if err := encodeJSON(stdout, toJSONScenarioResult(result)); err != nil {
				return reportUncaught(stderr, deps.Debug, err)
			}
		} else {
			printScenarioResult(stdout, result)
		}

		if reportOptions != nil {
			bundle, genErr := writeScenarioReport(runID, result, runStartedAt, finishedAt, platform, deviceID, envName, *reportOptions)
			if genErr != nil {
				return reportUncaught(stderr, deps.Debug, genErr)
			}
			_, _ = fmt.Fprintf(stderr, "Report: %s\n", bundle.BundleDir)
		}

		if result.Ok {
			return 0
		}
		return 1
	}

	def, err := script.ParseScript(string(content), externalEnv)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}
	preconditionsOverride := resolvePreconditionsOverride(cliPreconditionsFlag, runner.PreconditionsNoOverride, fromConfigPreconditions)

	scriptRunner := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:             built.Orchestra,
		Clock:                 deps.clock(),
		Logger:                deps.logger(),
		RunID:                 runID,
		Commands:              runner.DefaultCommands(),
		FlowLoader:            fsFlowLoader{},
		PreconditionsOverride: preconditionsOverride,
		NetworkCapture:        networkCapture,
	})
	result, err := scriptRunner.Run(ctx, def)
	finishedAt := time.Now()
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}

	if jsonOutput {
		if err := encodeJSON(stdout, toJSONScriptResult(result)); err != nil {
			return reportUncaught(stderr, deps.Debug, err)
		}
	} else {
		printTextResult(stdout, result)
	}

	if reportOptions != nil {
		bundle, genErr := writeReport(runID, result, runStartedAt, finishedAt, platform, deviceID, envName, *reportOptions)
		if genErr != nil {
			return reportUncaught(stderr, deps.Debug, genErr)
		}
		_, _ = fmt.Fprintf(stderr, "Report: %s\n", bundle.BundleDir)
	}

	if result.Ok {
		return 0
	}
	return 1
}

// reportUncaught writes err to stderr as "error [CODE]: <message>",
// followed by an actionable remedy on its own line when err carries
// one, with a stack trace appended when debug is set. It is the
// catch-all for an error that aborted a run before it produced a
// ScriptResult. It always returns 1.
func reportUncaught(stderr io.Writer, debugEnabled bool, err error) int {
	code, message, remedy := diag.Describe(err)
	_, _ = fmt.Fprintf(stderr, "error [%s]: %s\n", code, message)
	if remedy != "" {
		_, _ = fmt.Fprintln(stderr, remedy)
	}
	if debugEnabled {
		_, _ = stderr.Write(debug.Stack())
	}
	return 1
}
