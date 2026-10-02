package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"dev.atomyx/internal/runfolder"
)

// ErrInvalidRunID is returned for an id that is not a bare run id.
var ErrInvalidRunID = errors.New("run id must be a bare id without path separators")

// ErrRunReadOnly is returned when a summary update targets a versioned
// run folder that carries no summary, such as the run of a CLI script.
var ErrRunReadOnly = errors.New("run is stored as a versioned run folder that has no summary field")

// ErrRunLive is returned when a summary update targets a run that
// another process is still writing.
var ErrRunLive = errors.New("run is still being written by another process")

// DamagedRunError reports a run whose stored record exists but cannot
// be read. It names the run so a caller can tell which one failed.
type DamagedRunError struct {
	ID  string
	Err error
}

func (e *DamagedRunError) Error() string { return fmt.Sprintf("run %s is damaged: %v", e.ID, e.Err) }
func (e *DamagedRunError) Unwrap() error { return e.Err }

// RunListing is the result of RunCatalog.List. Runs is always usable.
// Skipped holds one entry per run record that could not be read and is
// therefore absent from Runs. A run that is unreadable in one form but
// readable in the other is in Runs and not in Skipped.
type RunListing struct {
	Runs    []runfolder.Meta
	Skipped []runfolder.SkippedRun
}

// RunCatalog reads runs from both on-disk forms: versioned run folders
// first, then flat legacy records. A run id present in both forms
// resolves to the versioned folder, unless that folder is damaged and
// the flat record is readable. Every meta it returns is settled through
// the catalog's Liveness, so an unfinished run is either running, with
// no result and no outcome, or interrupted.
//
// Its only writes are Delete and SetSummary.
type RunCatalog struct {
	versioned *runfolder.Reader
	root      string
	legacy    *LegacyRunReader
	live      runfolder.Liveness
}

// NewRunCatalog returns a catalog over store. root is the directory
// that holds versioned run folders, and an empty root means the catalog
// has no versioned runs and serves flat records alone. live decides
// which unfinished runs are still running, and nil means none are.
func NewRunCatalog(store Storage, root string, live runfolder.Liveness) *RunCatalog {
	c := &RunCatalog{legacy: NewLegacyRunReader(store), live: live}
	if root != "" {
		c.root = root
		c.versioned = runfolder.NewReader(root)
	}
	return c
}

// Get returns the run with the given id, preferring the versioned
// folder, and reports false with a nil error when neither form exists.
// A run that exists only in a damaged form returns a *DamagedRunError.
func (c *RunCatalog) Get(id string) (runfolder.Meta, bool, error) {
	var damaged error
	if c.versioned != nil {
		meta, ok, err := c.versioned.Get(id)
		if ok {
			return meta.Settle(c.live), true, nil
		}
		damaged = err
	}
	meta, ok, err := c.legacy.Get(id)
	if ok {
		return meta.Settle(c.live), true, nil
	}
	if damaged == nil {
		damaged = err
	}
	if damaged != nil {
		return runfolder.Meta{}, false, &DamagedRunError{ID: id, Err: damaged}
	}
	return runfolder.Meta{}, false, nil
}

// List returns every readable run from both forms, one entry per id
// with the versioned form winning, sorted by StartedAt descending and
// then by id ascending.
func (c *RunCatalog) List() (RunListing, error) {
	var listing RunListing
	seen := map[string]bool{}
	var versionedSkipped []runfolder.SkippedRun
	if c.versioned != nil {
		metas, skipped, err := c.versioned.List()
		if err != nil {
			return RunListing{}, err
		}
		for _, m := range metas {
			seen[m.RunID] = true
			listing.Runs = append(listing.Runs, m.Settle(c.live))
		}
		versionedSkipped = skipped
	}
	legacy, legacySkipped, err := c.legacy.List()
	if err != nil {
		return RunListing{}, err
	}
	legacyIDs := map[string]bool{}
	for _, m := range legacy {
		legacyIDs[m.RunID] = true
		if !seen[m.RunID] {
			listing.Runs = append(listing.Runs, m.Settle(c.live))
		}
	}
	for _, s := range versionedSkipped {
		if !legacyIDs[s.ID] {
			listing.Skipped = append(listing.Skipped, s)
		}
	}
	listing.Skipped = append(listing.Skipped, legacySkipped...)
	sort.SliceStable(listing.Runs, func(i, j int) bool {
		a, b := listing.Runs[i], listing.Runs[j]
		if a.StartedAt != b.StartedAt {
			return a.StartedAt > b.StartedAt
		}
		return a.RunID < b.RunID
	})
	return listing, nil
}

// Delete removes the run with the given id in whichever forms exist and
// reports whether any did. A form that disappears while it is being
// removed counts as already removed. When a form is removed and the
// other fails for any reason other than being absent, Delete returns
// both the true result and the failure.
func (c *RunCatalog) Delete(id string) (bool, error) {
	var removed bool
	var errs []error
	if c.versioned != nil {
		ok, err := runfolder.RemoveRun(c.root, id)
		removed = removed || ok
		errs = append(errs, err)
	}
	ok, err := c.deleteLegacy(id)
	removed = removed || ok
	errs = append(errs, err)
	return removed, errors.Join(errs...)
}

// deleteLegacy removes the flat record for id. A damaged record is
// still removed, since existence is all that matters for deletion, and
// a record that is already gone is not an error.
func (c *RunCatalog) deleteLegacy(id string) (bool, error) {
	if !runfolder.ValidRunID(id) {
		return false, nil
	}
	var raw json.RawMessage
	if ok, err := c.legacy.store.Load(RunKey(id), &raw); !ok || err != nil {
		return false, ignoreNotExist(err)
	}
	if err := c.legacy.store.Delete(RunKey(id)); err != nil {
		return false, ignoreNotExist(err)
	}
	return true, nil
}

func ignoreNotExist(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// SetSummary replaces the summary of the run with the given id and
// reports whether the run exists. It edits the versioned folder when
// one exists and the flat record otherwise. It returns ErrInvalidRunID
// for an id that is not a bare run id. A versioned run with no summary
// field returns ErrRunReadOnly, and one whose writer is still running
// returns ErrRunLive.
func (c *RunCatalog) SetSummary(id, summary string) (bool, error) {
	if !runfolder.ValidRunID(id) {
		return false, ErrInvalidRunID
	}
	if c.versioned != nil && c.versioned.Has(id) {
		ok, err := runfolder.SetSummary(c.root, id, summary)
		switch {
		case errors.Is(err, runfolder.ErrNoSummary):
			return ok, ErrRunReadOnly
		case errors.Is(err, runfolder.ErrRunLive):
			return ok, ErrRunLive
		}
		return ok, err
	}
	var record PersistedRun
	ok, err := c.legacy.store.Load(RunKey(id), &record)
	if err != nil || !ok {
		return false, err
	}
	record.Summary = &summary
	if err := c.legacy.store.Save(RunKey(id), record); err != nil {
		return false, err
	}
	return true, nil
}
