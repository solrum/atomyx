package storage

import (
	"encoding/json"
	"errors"
	"sort"

	"dev.atomyx/internal/runfolder"
)

// RunListing is the result of RunCatalog.List. Runs is always usable.
// Skipped holds one error per run record that could not be read and is
// therefore absent from Runs.
type RunListing struct {
	Runs    []runfolder.Meta
	Skipped []error
}

// RunCatalog reads runs from both on-disk forms: versioned run folders
// first, then flat legacy records. A run id present in both forms
// resolves to the versioned folder. The catalog never writes, and its
// only deletion is Delete.
//
// A catalog over a Storage that exposes no filesystem root has no
// versioned runs and serves legacy records alone.
type RunCatalog struct {
	versioned *runfolder.Reader
	root      string
	legacy    *LegacyRunReader
}

// Rooted is implemented by a Storage that keeps its records under a
// filesystem directory. That directory is also where versioned run
// folders live.
type Rooted interface {
	Root() string
}

// NewRunCatalog returns a catalog over store.
func NewRunCatalog(store Storage) *RunCatalog {
	c := &RunCatalog{legacy: NewLegacyRunReader(store)}
	if rooted, ok := store.(Rooted); ok && rooted.Root() != "" {
		c.root = rooted.Root()
		c.versioned = runfolder.NewReader(c.root)
	}
	return c
}

// Get returns the run with the given id, preferring the versioned
// folder, and reports false with a nil error when neither form exists.
func (c *RunCatalog) Get(id string) (runfolder.Meta, bool, error) {
	if c.versioned != nil {
		meta, ok, err := c.versioned.Get(id)
		if err != nil || ok {
			return meta, ok, err
		}
	}
	return c.legacy.Get(id)
}

// List returns every run from both forms, one entry per id with the
// versioned form winning, sorted by StartedAt descending and then by
// id ascending.
func (c *RunCatalog) List() (RunListing, error) {
	var listing RunListing
	seen := map[string]bool{}
	if c.versioned != nil {
		metas, err := c.versioned.List()
		for _, m := range metas {
			seen[m.RunID] = true
			listing.Runs = append(listing.Runs, m)
		}
		if err != nil {
			listing.Skipped = append(listing.Skipped, err)
		}
	}
	legacy, skipped, err := c.legacy.List()
	if err != nil {
		return RunListing{}, err
	}
	listing.Skipped = append(listing.Skipped, skipped...)
	for _, m := range legacy {
		if !seen[m.RunID] {
			listing.Runs = append(listing.Runs, m)
		}
	}
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
// removed counts as already removed.
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
// still removed, since existence is all that matters for deletion.
func (c *RunCatalog) deleteLegacy(id string) (bool, error) {
	if !isBareRecordName(id) {
		return false, nil
	}
	var raw json.RawMessage
	if ok, err := c.legacy.store.Load(RunKey(id), &raw); err != nil || !ok {
		return false, err
	}
	if err := c.legacy.store.Delete(RunKey(id)); err != nil {
		return false, err
	}
	return true, nil
}
