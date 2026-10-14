package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"dev.atomyx/internal/runfolder"
)

// PruneCriteria selects which runs a prune removes. Keep retains the
// newest Keep top-level runs, and OlderThan selects units that started
// more than that long before Now. When both are set a unit must satisfy
// both to be selected, and when neither is set nothing is selected.
type PruneCriteria struct {
	Keep      int
	OlderThan time.Duration
	// Now is the instant OlderThan is measured from.
	Now time.Time
}

// PruneUnit is a run and the worker runs that belong to it. A prune
// keeps or deletes a unit as a whole, so a kept suite index never points
// at a deleted worker. Members lists the root run id first, then the
// workers in id order.
//
// Live is true when any member is still being written, so the unit must
// not be deleted.
type PruneUnit struct {
	Root    runfolder.Meta
	Members []string
	Live    bool
}

// PrunePlan is the result of RunCatalog.PlanPrune. Units are the units
// the criteria select, newest first. Skipped holds the run records the
// catalog could not read, which a prune never deletes.
type PrunePlan struct {
	Units   []PruneUnit
	Skipped []runfolder.SkippedRun
}

// SelectPrune returns the units of runs that criteria select, newest
// first.
//
// A worker belongs to the unit of its parent, named by its ParentRunID or
// by a worker link in workers, which maps a run id to the worker run ids
// its index lists. A parent or worker that is not in runs is ignored, so
// a worker whose parent is absent is its own unit. The age of a unit is
// the start time of its root run, and a unit counts once toward Keep.
func SelectPrune(runs []runfolder.Meta, workers map[string][]string, criteria PruneCriteria) []PruneUnit {
	if criteria.Keep <= 0 && criteria.OlderThan <= 0 {
		return nil
	}
	byID := make(map[string]runfolder.Meta, len(runs))
	for _, m := range runs {
		byID[m.RunID] = m
	}
	parentOf := parentLinks(byID, workers)

	units := map[string]*PruneUnit{}
	for _, m := range runs {
		root, ok := unitRoot(m.RunID, parentOf)
		if !ok {
			continue
		}
		unit := units[root]
		if unit == nil {
			unit = &PruneUnit{Root: byID[root], Members: []string{root}}
			units[root] = unit
		}
		if m.RunID != root {
			unit.Members = append(unit.Members, m.RunID)
		}
		if m.CompletedAt == nil && m.Result == "" {
			unit.Live = true
		}
	}

	ordered := make([]*PruneUnit, 0, len(units))
	for _, u := range units {
		sort.Strings(u.Members[1:])
		ordered = append(ordered, u)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Root, ordered[j].Root
		if a.StartedAt != b.StartedAt {
			return a.StartedAt > b.StartedAt
		}
		return a.RunID < b.RunID
	})

	var selected []PruneUnit
	for rank, u := range ordered {
		if criteria.Keep > 0 && rank < criteria.Keep {
			continue
		}
		if criteria.OlderThan > 0 && !startedBefore(u.Root, criteria.Now.Add(-criteria.OlderThan)) {
			continue
		}
		selected = append(selected, *u)
	}
	return selected
}

func startedBefore(m runfolder.Meta, cutoff time.Time) bool {
	return m.StartedAt < cutoff.UnixMilli()
}

// parentLinks maps each worker run id to the id of the run it belongs
// to. A worker's own ParentRunID wins over an index link, and a link to a
// run that is not listed is dropped.
func parentLinks(byID map[string]runfolder.Meta, workers map[string][]string) map[string]string {
	parentOf := map[string]string{}
	parents := make([]string, 0, len(workers))
	for parent := range workers {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	for _, parent := range parents {
		if _, ok := byID[parent]; !ok {
			continue
		}
		for _, w := range workers[parent] {
			if _, ok := byID[w]; !ok || w == parent {
				continue
			}
			if _, claimed := parentOf[w]; !claimed {
				parentOf[w] = parent
			}
		}
	}
	for id, m := range byID {
		if _, ok := byID[m.ParentRunID]; ok && m.ParentRunID != "" && m.ParentRunID != id {
			parentOf[id] = m.ParentRunID
		}
	}
	return parentOf
}

// unitRoot follows parent links from id to the run that has no parent. It
// reports false for a run caught in a parent cycle, which belongs to no
// unit and is therefore never selected.
func unitRoot(id string, parentOf map[string]string) (string, bool) {
	for hops := 0; hops <= len(parentOf); hops++ {
		parent, ok := parentOf[id]
		if !ok {
			return id, true
		}
		id = parent
	}
	return "", false
}

// PlanPrune lists the runs and returns the units that criteria select.
// It reads the index of every suite run to find the workers it lists, and
// an index that is missing or unreadable adds no links. It deletes
// nothing.
func (c *RunCatalog) PlanPrune(criteria PruneCriteria) (PrunePlan, error) {
	listing, err := c.List()
	if err != nil {
		return PrunePlan{}, err
	}
	return PrunePlan{
		Units:   SelectPrune(listing.Runs, c.workerLinks(listing.Runs), criteria),
		Skipped: listing.Skipped,
	}, nil
}

func (c *RunCatalog) workerLinks(runs []runfolder.Meta) map[string][]string {
	if c.versioned == nil {
		return nil
	}
	links := map[string][]string{}
	for _, m := range runs {
		if m.Kind != runfolder.KindSuite {
			continue
		}
		index, err := runfolder.ReadIndex(runfolder.RunDir(c.root, m.RunID))
		if err != nil {
			continue
		}
		for _, entry := range index.Entries {
			for _, attempt := range entry.Attempts {
				if attempt.WorkerRunID != "" {
					links[m.RunID] = append(links[m.RunID], attempt.WorkerRunID)
				}
			}
		}
	}
	return links
}

// DeletePruneUnit deletes every run of unit, the root first and then its
// workers.
//
// It checks every member for a live writer before deleting any, and when
// one is live it deletes nothing and returns an error that satisfies
// errors.Is(err, ErrRunLive). A member that is already gone counts as
// deleted. When the root cannot be deleted the call returns that error
// at once and leaves every worker in place, so a kept parent never has
// an index that names a missing worker. When a worker cannot be deleted
// the remaining workers are still attempted and every failure is
// returned together.
func (c *RunCatalog) DeletePruneUnit(unit PruneUnit) error {
	if c.live != nil {
		for _, id := range unit.Members {
			if c.live.IsLive(id) {
				return fmt.Errorf("run %s: %w", id, ErrRunLive)
			}
		}
	}
	var errs []error
	for i, id := range unit.Members {
		if _, err := c.Delete(id); err != nil && !errors.Is(err, fs.ErrNotExist) {
			if i == 0 {
				return fmt.Errorf("run %s: %w", id, err)
			}
			errs = append(errs, fmt.Errorf("run %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
