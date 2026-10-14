package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"dev.atomyx/internal/storage"
)

const hoursPerDay = 24

// Prune actions reported for each unit, in text and in JSON.
const (
	pruneActionDeleted     = "deleted"
	pruneActionWouldDelete = "wouldDelete"
	pruneActionSkippedLive = "skippedLive"
	pruneActionFailed      = "failed"
)

// runPruneJSON is the --json shape of runs prune. Units is present and
// never null.
type runPruneJSON struct {
	DryRun        bool               `json:"dryRun"`
	Keep          int                `json:"keep,omitempty"`
	OlderThanDays int                `json:"olderThanDays,omitempty"`
	Units         []runPruneUnitJSON `json:"units"`
	RunsDeleted   int                `json:"runsDeleted"`
	Skipped       int                `json:"skipped"`
}

type runPruneUnitJSON struct {
	RunID     string   `json:"runId"`
	StartedAt string   `json:"startedAt"`
	Runs      []string `json:"runs"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	Error     string   `json:"error,omitempty"`
}

// pruneRequest holds the validated flags of runs prune.
type pruneRequest struct {
	keep          int
	olderThanDays int
	dryRun        bool
	json          bool
}

func parsePruneRequest(flags map[string]FlagValue) (pruneRequest, error) {
	req := pruneRequest{dryRun: flagBool(flags, "--dry-run"), json: flagBool(flags, "--json")}
	var err error
	if req.keep, err = positiveIntFlag(flags, "--keep"); err != nil {
		return req, err
	}
	if req.olderThanDays, err = positiveIntFlag(flags, "--older-than"); err != nil {
		return req, err
	}
	if req.keep == 0 && req.olderThanDays == 0 {
		return req, errors.New("runs prune needs --keep, --older-than, or both, so it never deletes every run by default")
	}
	return req, nil
}

// positiveIntFlag reads an optional flag as a whole number of at least
// 1, and returns zero when the flag is absent.
func positiveIntFlag(flags map[string]FlagValue, name string) (int, error) {
	raw, ok := flagString(flags, name)
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s %q must be a whole number of at least 1", name, raw)
	}
	return n, nil
}

func (r pruneRequest) reason() string {
	switch {
	case r.keep > 0 && r.olderThanDays > 0:
		return fmt.Sprintf("outside the newest %d runs and older than %d days", r.keep, r.olderThanDays)
	case r.keep > 0:
		return fmt.Sprintf("outside the newest %d runs", r.keep)
	}
	return fmt.Sprintf("older than %d days", r.olderThanDays)
}

// runsPrune deletes the runs, with the workers that belong to them, that
// --keep and --older-than select. A run and its workers are kept or
// deleted as one unit, a run that is still being written is never
// deleted, and run records that cannot be read are left untouched.
// --dry-run reports the selection without deleting.
//
// Exit codes: 0 when every selected unit was deleted or the command ran
// with --dry-run, 1 when a deletion failed or was refused or the storage
// cannot be read, and 2 when the command is misused.
func runsPrune(ctx context.Context, deps Deps, catalog *storage.RunCatalog, _ string, flags map[string]FlagValue) int {
	req, err := parsePruneRequest(flags)
	if err != nil {
		return runsMisuse(deps, "%s", err)
	}
	plan, err := catalog.PlanPrune(storage.PruneCriteria{
		Keep:      req.keep,
		OlderThan: time.Duration(req.olderThanDays) * hoursPerDay * time.Hour,
		Now:       time.UnixMilli(deps.clock().NowMs()),
	})
	if err != nil {
		return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
	}

	doc := runPruneJSON{DryRun: req.dryRun, Keep: req.keep, OlderThanDays: req.olderThanDays, Units: []runPruneUnitJSON{}, Skipped: len(plan.Skipped)}
	failed := false
	for _, unit := range plan.Units {
		entry := runPruneUnitJSON{
			RunID: unit.Root.RunID, StartedAt: formatTimeMs(unit.Root.StartedAt),
			Runs: unit.Members, Reason: req.reason(),
		}
		action, err := applyPrune(catalog, unit, req.dryRun)
		entry.Action = action
		if err != nil {
			entry.Error = err.Error()
			failed = failed || !req.dryRun
		}
		if action == pruneActionDeleted {
			doc.RunsDeleted += len(unit.Members)
		}
		doc.Units = append(doc.Units, entry)
	}

	if req.json {
		if err := encodeJSON(deps.stdout(), doc); err != nil {
			return reportUncaught(ctx, deps.stderr(), deps.Debug, err)
		}
	} else {
		printPruneReport(deps, doc)
	}
	if failed {
		return 1
	}
	return 0
}

// applyPrune deletes unit, or only classifies it when dryRun is set, and
// returns the action taken. A unit with a live member returns
// pruneActionSkippedLive and an error saying why, and a failed deletion
// returns pruneActionFailed and its error.
func applyPrune(catalog *storage.RunCatalog, unit storage.PruneUnit, dryRun bool) (string, error) {
	if unit.Live {
		return pruneActionSkippedLive, errUnitLive
	}
	if dryRun {
		return pruneActionWouldDelete, nil
	}
	if err := catalog.DeletePruneUnit(unit); err != nil {
		if errors.Is(err, storage.ErrRunLive) {
			return pruneActionSkippedLive, errUnitLive
		}
		return pruneActionFailed, err
	}
	return pruneActionDeleted, nil
}

var errUnitLive = errors.New("a run in this group is still being written")

func printPruneReport(deps Deps, doc runPruneJSON) {
	out := deps.stdout()
	if len(doc.Units) == 0 {
		_, _ = fmt.Fprintln(out, "No runs match the prune criteria.")
	} else {
		table := newTable(out)
		_, _ = fmt.Fprintln(table, "ACTION\tRUN ID\tSTARTED\tRUNS\tWHY")
		for _, u := range doc.Units {
			why := u.Reason
			if u.Error != "" {
				why = u.Error
			}
			_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\n", pruneActionLabel(u.Action), u.RunID, u.StartedAt, len(u.Runs), why)
		}
		_ = table.Flush()
	}
	if footer := pruneSkippedFooter(doc.Skipped); footer != "" {
		_, _ = fmt.Fprintln(out, footer)
	}
}

func pruneActionLabel(action string) string {
	switch action {
	case pruneActionDeleted:
		return "deleted"
	case pruneActionWouldDelete:
		return "would delete"
	case pruneActionSkippedLive:
		return "skipped"
	}
	return "failed"
}

func pruneSkippedFooter(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 run record could not be read and was left untouched."
	}
	return fmt.Sprintf("%d run records could not be read and were left untouched.", n)
}
