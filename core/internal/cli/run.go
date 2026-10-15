package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/shutdown"
	"dev.atomyx/internal/stepstream"
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

	var configReport *config.Report
	if loaded != nil {
		configReport = loaded.Config.Report
	}
	output, err := resolveReportOptions(flags, configReport)
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

	externalEnv, committedEnv, err := buildExternalEnv(flags, resolvedEnvironment)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	// The external values are secret from here on, so an error that quotes
	// one while the script parses is masked before it reaches stderr.
	seed := runner.NewRunSecrets()
	seed.AddExternal(externalEnv, committedEnv)
	addEnvRefValues(seed, resolvedEnvironment)
	mask := seed.String
	fail := func(err error) int { return reportUncaughtMasked(ctx, stderr, deps.Debug, mask, err) }

	jsonOutput := flagBool(flags, "--json")

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fail(err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fail(err)
	}

	dbg, err := newRunDebug(deps, flags, func() ([]runner.BreakScript, error) {
		return breakScriptsOf(absPath, string(content), externalEnv)
	}, jsonOutput)
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			_, _ = fmt.Fprintf(stderr, "error: %s\n", usage.message)
			return 2
		}
		return fail(err)
	}

	pruneToRetention(deps, loaded, output.Root)

	// The device connects before the script or scenario is parsed.
	// When both the device is unreachable and the file fails to
	// parse, the device error is the one reported, because parsing
	// never runs until the connection succeeds.
	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, deviceID)
	if err != nil {
		return fail(err)
	}
	opened, err := borrowDebuggable(ctx, deps, "atomyx run", "atomyx run "+filepath.Base(absPath), desc, dbg.debugger())
	if err != nil {
		return fail(err)
	}
	defer opened.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	built := opened.Built()
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	opened.cancelOnLost(cancelRun)
	dbg.attach(runCtx, opened.Session, built.Orchestra)
	defer dbg.stop()

	videoLifecycle := deps.videoLifecycle(videoEnabled(flags, loaded))
	captureConfig := parseCaptureFlag(proxyFlag)
	networkCapture, err := netcapture.New(captureConfig)
	if err != nil {
		return fail(err)
	}

	var envName *string
	if resolvedEnvironment != nil {
		name := resolvedEnvironment.Name
		envName = &name
	}

	runStartedAt := time.Now()

	if script.IsScenarioYaml(string(content)) {
		scenarioDef, err := script.ParseScenario(string(content))
		if err != nil {
			return fail(err)
		}

		// The run folder is created after the file parses and before
		// the scenario runs. Its id correlates this run's live step
		// trace and names the folder holding its report.
		record, err := startRunRecord(output, runIdentity{
			Kind:       runfolder.KindSuite,
			Label:      runLabel(scenarioDef.Name, absPath),
			ScriptPath: absPath,
			TestKey:    testKeyOf(absPath),
			Device:     desc,
		})
		if err != nil {
			return fail(err)
		}
		defer record.end(ctx, stderr)
		record.secrets.AddExternal(externalEnv, committedEnv)
		addEnvRefValues(record.secrets, resolvedEnvironment)
		mask = record.secrets.String
		runLog := record.maskedLogger(deps.logger())
		opened.recordRun(record)
		record.beginVideo(runCtx, videoLifecycle, desc)

		fromScenarioPreconditions := runner.PreconditionsNoOverride
		if scenarioDef.Preconditions != nil {
			fromScenarioPreconditions = runner.PreconditionsOverride(*scenarioDef.Preconditions)
		}
		preconditionsOverride := resolvePreconditionsOverride(cliPreconditionsFlag, fromScenarioPreconditions, fromConfigPreconditions)

		childArtifacts := newArtifactSequence(runfolder.ArtifactsDir(record.Dir()))
		childArtifacts.onFile = dbg.noteFile
		scenarioRunner := scenario.NewScenarioRunner(scenario.ScenarioRunnerDeps{
			Orchestra:             built.Orchestra,
			Clock:                 deps.clock(),
			Logger:                runLog,
			RunID:                 record.ID(),
			NetworkCapture:        networkCapture,
			Commands:              runner.DefaultCommands(),
			LoadScript:            fsScenarioScriptLoader(filepath.Dir(absPath)),
			ExternalEnv:           externalEnv,
			ExternalCommitted:     committedEnv,
			SecretSink:            record.secrets,
			PreconditionsOverride: preconditionsOverride,
			ChildArtifacts:        childArtifacts.forTarget,
			StepGate:              dbg.gate(),
			SourceDir:             filepath.Dir(absPath),
			OnStep:                stepstream.NewSink(record.writer, runLog, stepstream.WithPublisher(opened.PublishStep), stepstream.WithMask(record.secrets.String)),
		})
		result, err := scenarioRunner.Run(runCtx, scenarioDef)
		finishedAt := time.Now()
		if err == nil {
			dbg.reportUnhit()
		}
		if opened.reportLost(stderr) {
			record.setOutcome(runfolder.OutcomeDeviceLost, nil)
			return 1
		}
		if err != nil {
			record.setErrorFrom(err)
			return fail(err)
		}
		record.setOutcome(outcomeForResult(result.Ok), nil)
		kept := record.finishVideo(ctx)

		if jsonOutput {
			payload := toJSONScenarioResult(result.Masked(mask))
			payload.RunFolder = record.Dir()
			payload.Recording = toJSONRecording(kept)
			if err := encodeJSON(stdout, payload); err != nil {
				return fail(err)
			}
			record.reportedInJSON = true
		} else {
			printScenarioResult(stdout, result.Masked(mask))
		}

		bundle, genErr := writeScenarioReport(record.ID(), result, runStartedAt, finishedAt, platform, deviceID, envName, kept, record.reportOptions())
		if genErr != nil {
			return fail(genErr)
		}
		printReportLocation(stderr, record, bundle)
		printVideoLocation(stderr, record, kept)

		if result.Ok {
			return 0
		}
		return 1
	}

	def, err := script.ParseScript(string(content), externalEnv, script.WithCommittedNames(committedEnv))
	if err != nil {
		return fail(err)
	}
	preconditionsOverride := resolvePreconditionsOverride(cliPreconditionsFlag, runner.PreconditionsNoOverride, fromConfigPreconditions)

	record, err := startRunRecord(output, runIdentity{
		Kind:       runfolder.KindScript,
		Label:      runLabel(def.Name, absPath),
		ScriptPath: absPath,
		TestKey:    testKeyOf(absPath),
		Device:     desc,
	})
	if err != nil {
		return fail(err)
	}
	defer record.end(ctx, stderr)
	record.secrets.AddExternal(externalEnv, committedEnv)
	addEnvRefValues(record.secrets, resolvedEnvironment)
	mask = record.secrets.String
	runLog := record.maskedLogger(deps.logger())
	opened.recordRun(record)
	record.beginVideo(runCtx, videoLifecycle, desc)

	artifacts := newLiveArtifacts(runfolder.ArtifactsDir(record.Dir()))
	artifacts.seq.onFile = dbg.noteFile
	scriptRunner := runner.NewScriptRunner(runner.ScriptRunnerDeps{
		Orchestra:             built.Orchestra,
		Clock:                 deps.clock(),
		Logger:                runLog,
		RunID:                 record.ID(),
		Commands:              runner.DefaultCommands(),
		FlowLoader:            fsFlowLoader{},
		PreconditionsOverride: preconditionsOverride,
		NetworkCapture:        networkCapture,
		Artifacts:             artifacts,
		StepGate:              dbg.gate(),
		SourcePath:            absPath,
		SecretSink:            record.secrets,
		OnStep:                stepstream.NewSink(record.writer, runLog, stepstream.WithPublisher(opened.PublishStep), stepstream.WithMask(record.secrets.String)),
		FailureCapture: runner.NewFailureBundle(runner.FailureBundleDeps{
			Orchestra: built.Orchestra, Clock: deps.clock(), Logger: runLog, Sink: artifacts,
		}),
	})
	result, err := scriptRunner.Run(runCtx, def)
	finishedAt := time.Now()
	if err == nil {
		dbg.reportUnhit()
	}
	if opened.reportLost(stderr) {
		record.setOutcome(runfolder.OutcomeDeviceLost, nil)
		return 1
	}
	if err != nil {
		record.setErrorFrom(err)
		return fail(err)
	}
	record.setOutcome(outcomeForScript(result), scriptMeta(result.TotalSteps, result.FailedAtStep))
	record.setErrorCode(result.FailureCode())
	kept := record.finishVideo(ctx)

	if jsonOutput {
		payload := toJSONScriptResult(result.Masked(mask))
		payload.RunFolder = record.Dir()
		payload.Recording = toJSONRecording(kept)
		if err := encodeJSON(stdout, payload); err != nil {
			return fail(err)
		}
		record.reportedInJSON = true
	} else {
		printTextResult(stdout, result.Masked(mask))
	}

	bundle, genErr := writeReport(record.ID(), result, runStartedAt, finishedAt, platform, deviceID, envName, kept, record.reportOptions())
	if genErr != nil {
		return fail(genErr)
	}
	printReportLocation(stderr, record, bundle)
	printVideoLocation(stderr, record, kept)

	if result.Ok {
		return 0
	}
	return 1
}

// reportUncaught writes err to stderr as "error [CODE]: <message>",
// followed by an actionable remedy on its own line when err carries
// one, with a stack trace appended when debug is set. It is the
// catch-all for an error that aborted a run before it produced a
// ScriptResult. It returns 1, except when a stop signal canceled ctx:
// then it prints a single "interrupted" line and returns the
// signal's conventional exit code.
func reportUncaught(ctx context.Context, stderr io.Writer, debugEnabled bool, err error) int {
	return reportUncaughtMasked(ctx, stderr, debugEnabled, nil, err)
}

// reportUncaughtMasked is reportUncaught with the message and remedy
// passed through mask first, so an error that quotes a secret value never
// reaches the terminal. A nil mask leaves the text as it is.
func reportUncaughtMasked(ctx context.Context, stderr io.Writer, debugEnabled bool, mask func(string) string, err error) int {
	if code, ok := shutdown.InterruptExitCode(ctx); ok {
		_, _ = fmt.Fprintln(stderr, "interrupted")
		return code
	}
	code, message, remedy := diag.Describe(err)
	if mask != nil {
		message, remedy = mask(message), mask(remedy)
	}
	_, _ = fmt.Fprintf(stderr, "error [%s]: %s\n", code, message)
	if remedy != "" {
		_, _ = fmt.Fprintln(stderr, remedy)
	}
	if debugEnabled {
		_, _ = stderr.Write(debug.Stack())
	}
	return 1
}
