package cli

import (
	"context"
	"fmt"

	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

// runListJSON is the --json shape of runs list. Runs is present and
// never null.
type runListJSON struct {
	Runs    []runListEntryJSON `json:"runs"`
	Skipped int                `json:"skipped"`
}

type runListEntryJSON struct {
	RunID      string `json:"runId"`
	StartedAt  string `json:"startedAt"`
	Kind       string `json:"kind"`
	TestKey    string `json:"testKey,omitempty"`
	Result     string `json:"result"`
	Outcome    string `json:"outcome,omitempty"`
	DurationMs *int64 `json:"durationMs,omitempty"`
}

// runsList prints the recorded runs, newest first, that match the
// --test and --since filters.
//
// Exit codes: 0 on success, 1 when the storage cannot be read, and 2
// when the command is misused.
func runsList(ctx context.Context, deps Deps, catalog *storage.RunCatalog, _ string, flags map[string]FlagValue) int {
	sinceMs, err := parseSinceFlag(flags)
	if err != nil {
		return runsMisuse(deps, "%s", err)
	}
	test, _ := flagString(flags, "--test")
	listing, err := catalog.List()
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	var runs []runfolder.Meta
	for _, m := range listing.Runs {
		if sinceMs > 0 && m.StartedAt < sinceMs {
			continue
		}
		if test != "" && runKey(m) != test {
			continue
		}
		runs = append(runs, m)
	}
	if flagBool(flags, "--json") {
		return printRunList(ctx, deps, runs, len(listing.Skipped))
	}
	return printRunTable(deps, runs, len(listing.Skipped))
}

func runResultLabel(m runfolder.Meta) string {
	if m.Result == "" {
		return "running"
	}
	return string(m.Result)
}

func printRunList(ctx context.Context, deps Deps, runs []runfolder.Meta, skipped int) int {
	doc := runListJSON{Runs: make([]runListEntryJSON, 0, len(runs)), Skipped: skipped}
	for _, m := range runs {
		entry := runListEntryJSON{
			RunID:     m.RunID,
			StartedAt: formatTimeMs(m.StartedAt),
			Kind:      string(m.Kind),
			TestKey:   runKey(m),
			Result:    runResultLabel(m),
			Outcome:   string(m.Outcome),
		}
		if m.CompletedAt != nil {
			d := *m.CompletedAt - m.StartedAt
			entry.DurationMs = &d
		}
		doc.Runs = append(doc.Runs, entry)
	}
	if err := encodeJSON(deps.stdout(), doc); err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}
	return 0
}

func printRunTable(deps Deps, runs []runfolder.Meta, skipped int) int {
	out := deps.stdout()
	if len(runs) == 0 {
		_, _ = fmt.Fprintln(out, "No runs found.")
	} else {
		table := newTable(out)
		_, _ = fmt.Fprintln(table, "RUN ID\tSTARTED\tKIND\tTEST\tRESULT\tDURATION")
		for _, m := range runs {
			duration := "-"
			if m.CompletedAt != nil {
				duration = formatDurationMs(*m.CompletedAt - m.StartedAt)
			}
			_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
				m.RunID, formatTimeMs(m.StartedAt), m.Kind, dash(runKey(m)), runResultLabel(m), duration)
		}
		_ = table.Flush()
	}
	if footer := skippedFooter(skipped); footer != "" {
		_, _ = fmt.Fprintln(out, footer)
	}
	return 0
}
