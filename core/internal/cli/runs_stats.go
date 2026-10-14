package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dev.atomyx/internal/history"
	"dev.atomyx/internal/storage"
)

// runStatsJSON is the --json shape of runs stats. Tests is present and
// never null.
type runStatsJSON struct {
	Tests   []runStatsEntryJSON `json:"tests"`
	Skipped int                 `json:"skipped"`
}

type runStatsEntryJSON struct {
	TestKey       string   `json:"testKey"`
	Runs          int      `json:"runs"`
	Passed        int      `json:"passed"`
	Failed        int      `json:"failed"`
	Flaky         int      `json:"flaky"`
	FlakyRate     float64  `json:"flakyRate"`
	LastOutcomes  []string `json:"lastOutcomes"`
	DurationP50Ms int64    `json:"durationP50Ms"`
	DurationP95Ms int64    `json:"durationP95Ms"`
}

// runsStats prints the outcome statistics of every test key in the
// window the flags select, one row per key, flakiest first.
//
// Exit codes: 0 on success, 1 when the storage cannot be read, and 2
// when the command is misused.
func runsStats(ctx context.Context, deps Deps, catalog *storage.RunCatalog, root string, flags map[string]FlagValue) int {
	window, err := statsWindow(flags)
	if err != nil {
		return runsMisuse(deps, "%s", err)
	}
	source := &catalogSource{catalog: catalog, root: root}
	records, err := history.Collect(source.source())
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	stats := history.Aggregate(records, window)
	if test, ok := flagString(flags, "--test"); ok {
		stats = filterStats(stats, test)
	}
	sortStats(stats)
	if flagBool(flags, "--json") {
		return printStatsJSON(ctx, deps, stats, source.skipped)
	}
	return printStatsTable(deps, stats, source.skipped)
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

func filterStats(stats []history.TestStats, key string) []history.TestStats {
	kept := stats[:0]
	for _, s := range stats {
		if s.Key == key {
			kept = append(kept, s)
		}
	}
	return kept
}

// sortStats orders stats by flaky rate, then failures, both descending,
// then by key so the order is deterministic.
func sortStats(stats []history.TestStats) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.FlakyRate != b.FlakyRate {
			return a.FlakyRate > b.FlakyRate
		}
		if a.Failed != b.Failed {
			return a.Failed > b.Failed
		}
		return a.Key < b.Key
	})
}

func printStatsJSON(ctx context.Context, deps Deps, stats []history.TestStats, skipped int) int {
	doc := runStatsJSON{Tests: make([]runStatsEntryJSON, 0, len(stats)), Skipped: skipped}
	for _, s := range stats {
		last := make([]string, len(s.LastOutcomes))
		for i, o := range s.LastOutcomes {
			last[i] = string(o)
		}
		doc.Tests = append(doc.Tests, runStatsEntryJSON{
			TestKey: s.Key, Runs: s.Runs, Passed: s.Passed, Failed: s.Failed, Flaky: s.Flaky,
			FlakyRate: s.FlakyRate, LastOutcomes: last,
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
		_, _ = fmt.Fprintln(table, "TEST\tRUNS\tPASSED\tFAILED\tFLAKY\tFLAKY RATE\tP50\tP95\tLAST")
		for _, s := range stats {
			_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\t%.1f%%\t%s\t%s\t%s\n",
				s.Key, s.Runs, s.Passed, s.Failed, s.Flaky, s.FlakyRate*100,
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
