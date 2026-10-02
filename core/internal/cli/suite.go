package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/suite"
)

// RunSuite runs an ordered set of script and scenario entries
// resolved by a suite.Selector against a device built through
// deps.Factory, then renders the result to deps.Stdout, or reports a
// usage error to deps.Stderr.
//
// positional is the suite command's leading positional argument (a
// named config suite), nil when none was given.
//
// Exit codes mirror RunScript: 0 when every entry passed, 1 when the
// suite ran and at least one entry failed, 2 when the command itself
// was misused (an invalid or missing selector, an undeclared named
// suite, no flow matched the resolved selector).
func RunSuite(ctx context.Context, deps Deps, flags map[string]FlagValue, positional *string) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

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

	selector, err := resolveSuiteSelector(positional, flags, loaded)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	var configReport *config.Report
	if loaded != nil {
		configReport = loaded.Config.Report
	}
	output, err := resolveReportOptions(flags, configReport)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	externalEnv, err := buildExternalEnv(flags, resolvedEnvironment)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	var testRoots []string
	if loaded != nil && loaded.Config.TestRoots != nil {
		testRoots = *loaded.Config.TestRoots
	}
	resolvedFlows, err := suite.Resolve(ctx, selector, suite.DiscoveryOptions{Cwd: cwd, TestRoots: testRoots})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}
	if len(resolvedFlows) == 0 {
		_, _ = io.WriteString(stderr, "error: no flows matched the selector. Nothing to run.\n")
		return 2
	}

	entries := make([]scenario.SuiteEntry, len(resolvedFlows))
	for i, flow := range resolvedFlows {
		content, err := os.ReadFile(flow.Path)
		if err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
		entries[i] = scenario.SuiteEntry{Path: flow.Path, Content: string(content)}
	}

	platform := resolvePlatform(flags, resolvedEnvironment)
	deviceID := resolveDeviceID(flags, resolvedEnvironment)
	proxyFlag := resolveProxyFlag(flags, resolvedEnvironment)

	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, deviceID)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	opened, err := openDevice(ctx, deps, "atomyx suite", desc)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	defer opened.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	built := opened.Built()
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	opened.cancelOnLost(cancelRun)

	captureConfig := parseCaptureFlag(proxyFlag)
	networkCapture, err := netcapture.New(captureConfig)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}

	var envName *string
	if resolvedEnvironment != nil {
		name := resolvedEnvironment.Name
		envName = &name
	}

	resolvePreconditions := func(flowLevel *string) runner.PreconditionsOverride {
		scenarioOverride := runner.PreconditionsNoOverride
		if flowLevel != nil {
			scenarioOverride = runner.PreconditionsOverride(*flowLevel)
		}
		return resolvePreconditionsOverride(cliPreconditionsFlag, scenarioOverride, fromConfigPreconditions)
	}

	runStartedAt := time.Now()
	suiteLabel := "suite"
	if positional != nil {
		suiteLabel = *positional
	}
	// The run folder is created before the suite runs. Its id
	// correlates every entry's live step trace and names the folder
	// holding the suite's report.
	record, err := startRunRecord(output, runIdentity{
		Kind:   runfolder.KindSuite,
		Label:  suiteLabel,
		Device: desc,
	})
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	defer record.end(ctx, stderr)

	suiteRunner := scenario.NewSuiteRunner(scenario.SuiteRunnerDeps{
		Orchestra:        built.Orchestra,
		Clock:            deps.clock(),
		Logger:           deps.logger(),
		RunID:            record.ID(),
		NetworkCapture:   networkCapture,
		Commands:         runner.DefaultCommands(),
		CreateLoadScript: fsScenarioScriptLoader,
		ExternalEnv:      externalEnv,
	})

	result, err := suiteRunner.Run(runCtx, entries, &scenario.SuiteRunOptions{
		SuiteName:            positional,
		ResolvePreconditions: resolvePreconditions,
	})
	finishedAt := time.Now()
	if opened.reportLost(stderr) {
		record.setOutcome(runfolder.OutcomeDeviceLost, nil)
		return 1
	}
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	record.setOutcome(outcomeForResult(result.Ok), nil)

	printSuiteResult(stdout, result)

	metadata := buildReportMetadata(record.ID(), suiteLabel, runStartedAt, finishedAt, result.DurationMs, platform, deviceID, envName, nil)
	metadata.Command = report.ReportCommandSuite
	input := report.SuiteReportInput{
		Metadata:    metadata,
		Result:      result,
		Screenshots: suiteReportScreenshots(result),
	}
	bundle, genErr := report.NewFsReporter().Generate(input, record.reportOptions())
	if genErr != nil {
		return reportUncaught(ctx, stderr, deps.Debug, genErr)
	}
	printReportLocation(stderr, record, bundle)

	if result.Ok {
		return 0
	}
	return 1
}

// resolveSuiteSelector builds the suite.Selector from a named config
// suite, or from ad-hoc --tag / --glob flags. A named suite and
// ad-hoc flags are mutually exclusive, and at least one of the two
// must be given.
func resolveSuiteSelector(positional *string, flags map[string]FlagValue, loaded *config.LoadedConfig) (suite.Selector, error) {
	tagFlags := flagStrings(flags, "--tag")
	globFlags := flagStrings(flags, "--glob")
	hasAdhocSelector := len(tagFlags) > 0 || len(globFlags) > 0

	if positional != nil {
		if hasAdhocSelector {
			return suite.Selector{}, errors.New(
				"a named suite and ad-hoc --tag or --glob flags cannot be combined, " +
					"use a named suite, or use --tag or --glob alone",
			)
		}

		var suites map[string]config.SuiteEntry
		if loaded != nil && loaded.Config.Suites != nil {
			suites = *loaded.Config.Suites
		}
		entry, ok := suites[*positional]
		if !ok {
			return suite.Selector{}, fmt.Errorf(
				"suite %q is not defined in the project config, available suites: %s",
				*positional, availableSuites(suites),
			)
		}

		var tags, globs []string
		if entry.Tags != nil {
			tags = *entry.Tags
		}
		if entry.Globs != nil {
			globs = *entry.Globs
		}
		return suite.Selector{Tags: tags, Globs: globs}, nil
	}

	if !hasAdhocSelector {
		return suite.Selector{}, errors.New(
			"the suite command requires either a suite name declared in the " +
				"project config, or at least one --tag or --glob flag",
		)
	}

	return suite.Selector{Tags: tagFlags, Globs: globFlags}, nil
}

// availableSuites renders loaded.Config's declared suite names for
// the "Suite \"x\" is not defined" error, sorted rather than in
// source declaration order: unlike AtomyxConfig.EnvironmentOrder, the
// Go config loader does not track a suites block's declaration order.
func availableSuites(suites map[string]config.SuiteEntry) string {
	if len(suites) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(suites))
	for name := range suites {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// printSuiteResult renders result to w in the suite command's output
// format: a PASS or FAIL summary line, a passed-of-total count with
// duration, then one line per entry with its status, kind, path, and
// duration, and any error message indented beneath it. This text is
// user-facing, so its wording and layout must not change without
// updating every consumer that parses it.
func printSuiteResult(w io.Writer, result scenario.SuiteResult) {
	status := "FAIL"
	if result.Ok {
		status = "PASS"
	}
	label := ""
	if result.SuiteName != nil {
		label = fmt.Sprintf(" \"%s\"", *result.SuiteName)
	}
	_, _ = fmt.Fprintf(w, "\n%s: suite%s\n", status, label)
	_, _ = fmt.Fprintf(w, "  %d of %d flows passed (%dms)\n\n", result.Passed, result.Total, result.DurationMs)

	for _, entry := range result.Entries {
		entryStatus := "FAIL"
		if entry.Status == scenario.EntryPassed {
			entryStatus = "PASS"
		}
		_, _ = fmt.Fprintf(w, "  %s [%s] %s, %dms\n", entryStatus, entry.Kind, entry.Path, entry.DurationMs)
		if entry.Error != nil {
			_, _ = fmt.Fprintf(w, "    %s\n", *entry.Error)
		}
	}
	_, _ = io.WriteString(w, "\n")
}

// suiteReportScreenshots flattens every entry's screenshots out of a
// suite result. Each entry's scope carries its position in the
// suite's entry list, so two entries that run the same file still get
// distinct scopes; a scenario entry's child screenshots nest inside
// that entry's own scope via report.ScenarioChildScope, so they stay
// distinct from every other entry's scope too.
func suiteReportScreenshots(result scenario.SuiteResult) []report.ReportScreenshot {
	var out []report.ReportScreenshot
	for entryIndex, entry := range result.Entries {
		if entry.Result == nil {
			continue
		}
		scope := report.SuiteEntryScope(entryIndex, entry.Path)
		switch {
		case entry.Result.Scenario != nil:
			for _, child := range entry.Result.Scenario.Scripts {
				if child.Result == nil {
					continue
				}
				childScope := report.ScenarioChildScope(scope, child.ScriptIndex, child.ScriptPath)
				for _, shot := range child.Result.Artifacts.Screenshots() {
					out = append(out, report.ReportScreenshot{Scope: childScope, Label: shot.Label, Data: shot.Data, MimeType: shot.MimeType})
				}
			}
		case entry.Result.Script != nil:
			for _, shot := range entry.Result.Script.Artifacts.Screenshots() {
				out = append(out, report.ReportScreenshot{Scope: scope, Label: shot.Label, Data: shot.Data, MimeType: shot.MimeType})
			}
		}
	}
	return out
}
