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
	"dev.atomyx/internal/report"
	"dev.atomyx/internal/runfolder"
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
// With --device the whole suite runs in this process on that one device.
// With --json the result object goes to stdout and the human summary goes
// to deps.Stderr.
//
// Exit codes mirror RunScript: 0 when every entry passed, 1 when the
// suite ran and at least one entry failed, 2 when the command itself
// was misused (an invalid or missing selector, an undeclared named
// suite, no flow matched the resolved selector).
func RunSuite(ctx context.Context, deps Deps, flags map[string]FlagValue, positional *string) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

	if flagBool(flags, "--worker") {
		return runSuiteWorker(ctx, deps, flags, positional)
	}
	jsonOutput := flagBool(flags, "--json")
	humanOut := stdout
	if jsonOutput {
		humanOut = stderr
	}

	cfg, err := resolveSuiteSettings(flags, positional)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}
	selector, err := resolveSuiteSelector(positional, flags, cfg.loaded)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	cfg.cwd = cwd
	resolvedFlows, err := suite.Resolve(ctx, selector, suite.DiscoveryOptions{Cwd: cwd, TestRoots: cfg.testRoots})
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

	platform := resolvePlatform(flags, cfg.environment)
	deviceID := resolveDeviceID(flags, cfg.environment)

	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, deviceID)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	host, err := startSuiteHost(ctx, deps, cfg, resolveProxyFlag(flags, cfg.environment), "", desc, videoEnabled(flags, cfg.loaded))
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	defer host.close(ctx, stderr)
	opened, record := host.opened, host.record

	var envName *string
	if cfg.environment != nil {
		name := cfg.environment.Name
		envName = &name
	}
	suiteLabel := cfg.label

	runStartedAt := time.Now()
	result, err := host.runner.Run(host.runCtx, entries, host.runOptions(positional, 0)) //nolint:contextcheck // the host's run context is derived from ctx
	finishedAt := time.Now()
	indexEntries := suiteIndexEntries(result)
	indexErr := record.writer.WriteIndex(indexEntries)
	counts := suiteMetaCounts(result)
	if opened.reportLost(stderr) {
		record.setOutcome(runfolder.OutcomeDeviceLost, counts)
		return 1
	}
	if err != nil {
		record.setOutcome(runfolder.OutcomeError, counts)
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	record.setOutcome(outcomeForResult(result.Ok), counts)
	if indexErr != nil {
		return reportUncaught(ctx, stderr, deps.Debug, indexErr)
	}

	printSuiteResult(humanOut, result)
	if jsonOutput {
		payload := suiteJSONResult{Ok: result.Ok, RunFolder: record.Dir(), Entries: indexEntries}
		if err := encodeJSON(stdout, payload); err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
		record.reportedInJSON = true
	}

	metadata := buildReportMetadata(record.ID(), suiteLabel, runStartedAt, finishedAt, result.DurationMs, platform, deviceID, envName, nil)
	metadata.Command = report.ReportCommandSuite
	input := report.SuiteReportInput{
		Metadata:    metadata,
		Result:      result,
		Screenshots: suiteReportScreenshots(result),
		EntryVideos: indexEntryVideos(indexEntries),
	}
	bundle, genErr := report.NewFsReporter().Generate(input, record.reportOptions())
	if genErr != nil {
		return reportUncaught(ctx, stderr, deps.Debug, genErr)
	}
	printReportLocation(stderr, record, bundle)
	printVideoLocations(stderr, record.Dir(), input.EntryVideos)

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
	_, _ = fmt.Fprintf(w, "  %d of %d flows passed%s (%dms)\n\n", result.Passed, result.Total, suiteCountsNote(result), result.DurationMs)

	for _, entry := range result.Entries {
		entryStatus := "FAIL"
		if entry.Status == scenario.EntryPassed {
			entryStatus = "PASS"
		}
		_, _ = fmt.Fprintf(w, "  %s [%s] %s, %dms%s\n", entryStatus, entry.Kind, entry.Path, entry.DurationMs, entryNote(entry))
		if entry.Error != nil {
			_, _ = fmt.Fprintf(w, "    %s\n", *entry.Error)
		}
	}
	_, _ = io.WriteString(w, "\n")
}

// suiteCountsNote renders the flaky and quarantined counts that follow the
// passed count, or an empty string when both are zero.
func suiteCountsNote(result scenario.SuiteResult) string {
	var note string
	if result.Flaky > 0 {
		note += fmt.Sprintf(", %d flaky", result.Flaky)
	}
	if result.QuarantinedFailures > 0 {
		note += fmt.Sprintf(", %d quarantined failures", result.QuarantinedFailures)
	}
	return note
}

// entryNote renders what the retries and the quarantine list did to an
// entry, or an empty string for an entry that ran once and is not
// quarantined.
func entryNote(entry scenario.SuiteEntryResult) string {
	switch {
	case entry.Flaky:
		return fmt.Sprintf(" (flaky, passed on attempt %d)", len(entry.Attempts))
	case entry.QuarantinedFailure():
		return " (quarantined)"
	case len(entry.Attempts) > 1:
		return fmt.Sprintf(" (failed %d attempts)", len(entry.Attempts))
	}
	return ""
}

// suiteReportScreenshots flattens every entry's screenshots out of a
// suite result. Each entry's scope carries its position in the
// suite's entry list, so two entries that run the same file still get
// distinct scopes; a scenario entry's child screenshots nest inside
// that entry's own scope via report.ScenarioChildScope, so they stay
// distinct from every other entry's scope too. Every attempt of a retried
// entry contributes its screenshots under its own attempt scope, in the
// order they were taken, which keeps the file names the live artifact
// writer assigned valid.
func suiteReportScreenshots(result scenario.SuiteResult) []report.ReportScreenshot {
	var out []report.ReportScreenshot
	for entryIndex, entry := range result.Entries {
		attempts := entry.Attempts
		if len(attempts) == 0 {
			attempts = []scenario.SuiteAttempt{{Number: 1, Result: entry.Result}}
		}
		for _, attempt := range attempts {
			if attempt.Result == nil {
				continue
			}
			scope := report.SuiteAttemptScope(report.SuiteEntryScope(entryIndex, entry.Path), attempt.Number)
			out = append(out, outcomeScreenshots(scope, *attempt.Result)...)
		}
	}
	return out
}

// outcomeScreenshots flattens the screenshots of one attempt's outcome
// under scope. A scenario's child screenshots nest inside scope.
func outcomeScreenshots(scope string, outcome scenario.SuiteEntryOutcome) []report.ReportScreenshot {
	var out []report.ReportScreenshot
	switch {
	case outcome.Scenario != nil:
		for _, child := range outcome.Scenario.Scripts {
			if child.Result == nil {
				continue
			}
			childScope := report.ScenarioChildScope(scope, child.ScriptIndex, child.ScriptPath)
			for _, shot := range child.Result.Artifacts.Screenshots() {
				out = append(out, report.ReportScreenshot{Scope: childScope, Label: shot.Label, Data: shot.Data, MimeType: shot.MimeType})
			}
		}
	case outcome.Script != nil:
		for _, shot := range outcome.Script.Artifacts.Screenshots() {
			out = append(out, report.ReportScreenshot{Scope: scope, Label: shot.Label, Data: shot.Data, MimeType: shot.MimeType})
		}
	}
	return out
}
