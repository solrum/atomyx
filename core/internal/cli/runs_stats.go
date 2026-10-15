package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dev.atomyx/internal/history"
)

// runStatsJSON is the --json shape of runs stats. Tests is present and
// never null.
type runStatsJSON struct {
	Tests            []runStatsEntryJSON `json:"tests"`
	Skipped          int                 `json:"skipped"`
	InfraFailures    int                 `json:"infraFailures"`
	TestFailures     int                 `json:"testFailures"`
	InfraFailureRate float64             `json:"infraFailureRate"`
	AbortedRuns      int                 `json:"abortedRuns"`
	Attempts         int                 `json:"attempts"`
}

type runStatsEntryJSON struct {
	TestKey          string   `json:"testKey"`
	Runs             int      `json:"runs"`
	Passed           int      `json:"passed"`
	Failed           int      `json:"failed"`
	Flaky            int      `json:"flaky"`
	FlakyRate        float64  `json:"flakyRate"`
	InfraFailures    int      `json:"infraFailures"`
	TestFailures     int      `json:"testFailures"`
	InfraFailureRate float64  `json:"infraFailureRate"`
	AbortedRuns      int      `json:"abortedRuns"`
	Attempts         int      `json:"attempts"`
	LastOutcomes     []string `json:"lastOutcomes"`
	DurationP50Ms    int64    `json:"durationP50Ms"`
	DurationP95Ms    int64    `json:"durationP95Ms"`
}

// runsStats prints the outcome statistics of every test key in the
// window the flags select, one row per key, flakiest first.
//
// Exit codes: 0 on success, 1 when the storage cannot be read, and 2
// when the command is misused.
func runsStats(ctx context.Context, deps Deps, env runsEnv, flags map[string]FlagValue) int {
	window, err := statsWindow(flags)
	if err != nil {
		return runsMisuse(deps, "%s", err)
	}
	filter := history.Filter{Window: window}
	if test, ok := flagString(flags, "--test"); ok {
		filter.Key = test
	}
	result, err := history.Query(env.History, filter)
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	if flagBool(flags, "--json") {
		return printStatsJSON(ctx, deps, result)
	}
	return printStatsTable(deps, result.Tests, result.Skipped)
}

func statsWindow(flags map[string]FlagValue) (history.Window, error) {
	var window history.Window
	since, err := parseSinceFlag(flags)
	if err != nil {
		return window, err
	}
	window.SinceMs = since
	if raw, ok := flagString(flags, "--last"); ok {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 {
			return window, fmt.Errorf("--last %q must be a whole number of at least 1", raw)
		}
		window.LastN = n
	}
	return window, nil
}

func printStatsJSON(ctx context.Context, deps Deps, result history.Result) int {
	doc := runStatsJSON{
		Tests: make([]runStatsEntryJSON, 0, len(result.Tests)), Skipped: result.Skipped,
		InfraFailures: result.Totals.InfraFailures, TestFailures: result.Totals.TestFailures,
		InfraFailureRate: result.Totals.InfraFailureRate, AbortedRuns: result.Totals.AbortedRuns, Attempts: result.Totals.Attempts,
	}
	for _, s := range result.Tests {
		last := make([]string, len(s.LastOutcomes))
		for i, o := range s.LastOutcomes {
			last[i] = string(o)
		}
		doc.Tests = append(doc.Tests, runStatsEntryJSON{
			TestKey: s.Key, Runs: s.Runs, Passed: s.Passed, Failed: s.Failed, Flaky: s.Flaky,
			FlakyRate: s.FlakyRate, LastOutcomes: last,
			InfraFailures: s.InfraFailures, TestFailures: s.TestFailures, InfraFailureRate: s.InfraFailureRate, AbortedRuns: s.AbortedRuns, Attempts: s.Attempts,
			DurationP50Ms: s.DurationP50Ms, DurationP95Ms: s.DurationP95Ms,
		})
	}
	if err := encodeJSON(deps.stdout(), doc); err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	return 0
}

func printStatsTable(deps Deps, stats []history.TestStats, skipped int) int {
	out := deps.stdout()
	if len(stats) == 0 {
		_, _ = fmt.Fprintln(out, "No finished runs found.")
	} else {
		table := newTable(out)
		_, _ = fmt.Fprintln(table, "TEST\tRUNS\tPASSED\tFAILED\tINFRA FAILS\tTEST FAILS\tFLAKY\tFLAKY RATE\tINFRA RATE\tP50\tP95\tLAST")
		for _, s := range stats {
			_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f%%\t%.1f%%\t%s\t%s\t%s\n",
				s.Key, s.Runs, s.Passed, s.Failed, s.InfraFailures, s.TestFailures, s.Flaky, s.FlakyRate*100, s.InfraFailureRate*100,
				durationOrDash(s.DurationP50Ms), durationOrDash(s.DurationP95Ms), lastOutcomeLetters(s.LastOutcomes))
		}
		_ = table.Flush()
	}
	if footer := skippedFooter(skipped); footer != "" {
		_, _ = fmt.Fprintln(out, footer)
	}
	return 0
}

func durationOrDash(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return formatDurationMs(ms)
}

// lastOutcomeLetters renders outcomes, most recent first, as one letter
// each: P passed, F failed, K flaky.
func lastOutcomeLetters(outcomes []history.Outcome) string {
	var b strings.Builder
	for _, o := range outcomes {
		switch o {
		case history.OutcomeFailed:
			b.WriteByte('F')
		case history.OutcomeFlaky:
			b.WriteByte('K')
		default:
			b.WriteByte('P')
		}
	}
	return b.String()
}
