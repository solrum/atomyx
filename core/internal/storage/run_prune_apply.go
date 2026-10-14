package storage

import (
	"errors"
	"fmt"
	"os"
	"time"

	"dev.atomyx/internal/runfolder"
)

const hoursPerDay = 24

// NewPruneCriteria builds the criteria of a prune from a count of units to
// keep and an age in whole days. A zero value leaves that bound unset.
func NewPruneCriteria(keep, olderThanDays int, now time.Time) PruneCriteria {
	return PruneCriteria{
		Keep:      keep,
		OlderThan: time.Duration(olderThanDays) * hoursPerDay * time.Hour,
		Now:       now,
	}
}

// PruneAction is what a prune did with one unit.
type PruneAction string

const (
	PruneDeleted     PruneAction = "deleted"
	PruneWouldDelete PruneAction = "wouldDelete"
	PruneSkippedLive PruneAction = "skippedLive"
	PruneFailed      PruneAction = "failed"
)

// ErrPruneUnitLive describes a unit that was left alone because one of its
// runs is still being written.
var ErrPruneUnitLive = errors.New("a run in this group is still being written")

// PruneOutcome is the result for one selected unit. Err is set for a unit
// that was skipped as live or that failed to delete, and is nil otherwise.
type PruneOutcome struct {
	Unit   PruneUnit
	Action PruneAction
	Err    error
}

// PruneResult is the outcome of ApplyPrune. Outcomes follow the plan
// order, newest unit first. Skipped counts the run records that could not
// be read and were left untouched. RunsDeleted counts every deleted run,
// workers included.
type PruneResult struct {
	Outcomes    []PruneOutcome
	Skipped     int
	RunsDeleted int
}

// Deleted returns the units that were deleted.
func (r PruneResult) Deleted() []PruneUnit {
	var units []PruneUnit
	for _, o := range r.Outcomes {
		if o.Action == PruneDeleted {
			units = append(units, o.Unit)
		}
	}
	return units
}

// Failed reports whether any unit failed to delete or was refused as live.
func (r PruneResult) Failed() bool {
	for _, o := range r.Outcomes {
		if o.Err != nil {
			return true
		}
	}
	return false
}

// Warnings returns one human-readable line for every unit that was not
// deleted although the criteria selected it, plus one for unreadable run
// records. A deletion refused because a migration holds the storage lock
// is reported as a warning that says the prune will apply next time.
func (r PruneResult) Warnings() []string {
	var out []string
	for _, o := range r.Outcomes {
		if o.Err == nil {
			continue
		}
		switch {
		case o.Action == PruneSkippedLive:
			out = append(out, fmt.Sprintf("run %s is still being written and was kept", o.Unit.Root.RunID))
		case errors.Is(o.Err, ErrMigrationInProgress):
			out = append(out, fmt.Sprintf("run %s was kept because a run migration is in progress", o.Unit.Root.RunID))
		default:
			out = append(out, fmt.Sprintf("run %s could not be deleted: %v", o.Unit.Root.RunID, o.Err))
		}
	}
	switch r.Skipped {
	case 0:
	case 1:
		out = append(out, "1 run record could not be read and was left untouched")
	default:
		out = append(out, fmt.Sprintf("%d run records could not be read and were left untouched", r.Skipped))
	}
	return out
}

// ApplyPrune plans a prune over c with criteria and deletes every selected
// unit that is not live. With dryRun set it only classifies the units and
// deletes nothing. A unit with a live member is never deleted. The error
// is non-nil only when the catalog cannot be listed, and a unit that
// fails to delete is reported in its outcome instead.
func (c *RunCatalog) ApplyPrune(criteria PruneCriteria, dryRun bool) (PruneResult, error) {
	plan, err := c.PlanPrune(criteria)
	if err != nil {
		return PruneResult{}, err
	}
	result := PruneResult{Skipped: len(plan.Skipped), Outcomes: make([]PruneOutcome, 0, len(plan.Units))}
	for _, unit := range plan.Units {
		outcome := c.applyUnit(unit, dryRun)
		if outcome.Action == PruneDeleted {
			result.RunsDeleted += len(unit.Members)
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

func (c *RunCatalog) applyUnit(unit PruneUnit, dryRun bool) PruneOutcome {
	if unit.Live {
		return PruneOutcome{Unit: unit, Action: PruneSkippedLive, Err: ErrPruneUnitLive}
	}
	if dryRun {
		return PruneOutcome{Unit: unit, Action: PruneWouldDelete}
	}
	if err := c.DeletePruneUnit(unit); err != nil {
		if errors.Is(err, ErrRunLive) {
			return PruneOutcome{Unit: unit, Action: PruneSkippedLive, Err: ErrPruneUnitLive}
		}
		return PruneOutcome{Unit: unit, Action: PruneFailed, Err: err}
	}
	return PruneOutcome{Unit: unit, Action: PruneDeleted}
}

// PruneRoot prunes the run folders under root with criteria. A root that
// does not exist yet has nothing to prune and returns an empty result.
// Callers that write run folders into root use it to apply a retention
// policy before writing.
func PruneRoot(root string, criteria PruneCriteria) (PruneResult, error) {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return PruneResult{}, nil
	}
	store, err := NewFileStorage(FileStorageOptions{Root: root})
	if err != nil {
		return PruneResult{}, err
	}
	catalog := NewRunCatalog(store, root, runfolder.NewLockLiveness(root))
	return catalog.ApplyPrune(criteria, false)
}
