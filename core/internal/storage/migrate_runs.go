package storage

import (
	"errors"
	"fmt"
	"strings"

	"dev.atomyx/internal/runfolder"
)

// ErrMigrationInProgress is returned by a delete or a summary update
// while a run migration holds the migration lock. The caller repeats
// the call once the migration finishes.
var ErrMigrationInProgress = runfolder.ErrMigrationInProgress

// MigrationIssue names a flat record the migration did not move and why.
type MigrationIssue struct {
	ID     string
	Reason string
}

// MigrationReport says what one MigrateRuns call did, with every id
// sorted. A root with no flat records produces the zero report.
//
//   - Migrated holds the records whose folder this call created.
//   - AlreadyMigrated holds the records whose folder already exists
//     because an earlier call imported this record into it, whether or
//     not the folder was edited since, or because it holds an identical
//     meta. That is what an interrupted earlier call leaves. The
//     original is stale, so it is removed now.
//   - Conflicts holds the records whose id names a folder that was not
//     imported from that record and holds a different meta. Both are
//     left untouched.
//   - Damaged holds the records that could not be read or mapped, and
//     the records whose new folder did not read back equal to the
//     record. They are left untouched and the new folder is removed.
//   - ScratchRemoved counts the scratch folders an interrupted earlier
//     call left and this call deleted.
type MigrationReport struct {
	Migrated        []string
	AlreadyMigrated []string
	Conflicts       []MigrationIssue
	Damaged         []MigrationIssue
	ScratchRemoved  int
}

// Clean reports whether no record was left behind as a conflict or as
// damaged.
func (r MigrationReport) Clean() bool {
	return len(r.Conflicts) == 0 && len(r.Damaged) == 0
}

// MigrateRuns rewrites every flat run record in store as a run folder
// under root, the directory FileStorage keeps its records in. It is
// safe to run again after any interruption and changes nothing when no
// flat record is left.
//
// The whole call holds the migration lock exclusively. It waits for an
// editor that holds the shared form, calling onWait once first when it
// has to wait, and a delete or summary update that starts meanwhile is
// refused with ErrMigrationInProgress. A nil onWait is allowed.
//
// Each record is processed on its own. Its meta is built with the same
// mapping the compatibility reader uses and the legacy id stays the
// folder name. The meta also names the record it came from in its
// ImportedFrom field. The folder is written through runfolder.ImportRun,
// which reads it back and compares it with the source, and only then is
// the original deleted. A crash between those two steps leaves a
// verified folder and the original, and the next call finds a folder
// that names this record as its source, so it only deletes the
// original, even when the folder was edited in between. A record that
// cannot be read or mapped, a record whose folder fails the read-back,
// and a record whose id names a folder from another source are reported
// and left in place, and the pass continues with the next record.
//
// An error means the call stopped early because storage failed, for
// example a delete that was refused. The report holds what was done up
// to that point, and a later call finishes the rest.
func MigrateRuns(store Storage, root string, onWait func()) (MigrationReport, error) {
	return migrateRuns(store, root, onWait, runfolder.ImportRun)
}

type importFunc func(root string, meta runfolder.Meta) (runfolder.ImportOutcome, error)

func migrateRuns(store Storage, root string, onWait func(), importRun importFunc) (MigrationReport, error) {
	var report MigrationReport
	lock, err := runfolder.LockMigration(root, onWait)
	if err != nil {
		return report, err
	}
	defer func() { _ = lock.Release() }()

	report.ScratchRemoved, err = runfolder.RemoveImportScratch(root)
	if err != nil {
		return report, fmt.Errorf("removing scratch folders: %w", err)
	}
	keys, err := store.ListChildren(strings.TrimSuffix(RunKeyPrefix, "/"))
	if err != nil {
		return report, fmt.Errorf("listing flat run records: %w", err)
	}
	for _, key := range keys {
		id := strings.TrimPrefix(key, RunKeyPrefix)
		if err := migrateOne(store, root, id, importRun, &report); err != nil {
			return report, fmt.Errorf("migrating run %s: %w", id, err)
		}
	}
	return report, nil
}

func migrateOne(store Storage, root, id string, importRun importFunc, report *MigrationReport) error {
	if !runfolder.ValidRunID(id) {
		report.Damaged = append(report.Damaged, MigrationIssue{ID: id, Reason: "the record name is not a bare run id"})
		return nil
	}
	var record PersistedRun
	ok, err := store.Load(RunKey(id), &record)
	if err != nil {
		report.Damaged = append(report.Damaged, MigrationIssue{ID: id, Reason: err.Error()})
		return nil
	}
	if !ok {
		return nil
	}
	meta, err := legacyMeta(id, record)
	if err != nil {
		report.Damaged = append(report.Damaged, MigrationIssue{ID: id, Reason: err.Error()})
		return nil
	}
	meta.ImportedFrom = id
	outcome, err := importRun(root, meta)
	if errors.Is(err, runfolder.ErrImportConflict) {
		report.Conflicts = append(report.Conflicts, MigrationIssue{ID: id, Reason: err.Error()})
		return nil
	}
	if errors.Is(err, runfolder.ErrImportMismatch) {
		report.Damaged = append(report.Damaged, MigrationIssue{ID: id, Reason: err.Error()})
		return nil
	}
	if err != nil {
		return err
	}
	if err := store.Delete(RunKey(id)); err != nil {
		return fmt.Errorf("removing the original: %w", err)
	}
	if outcome == runfolder.ImportExisting {
		report.AlreadyMigrated = append(report.AlreadyMigrated, id)
	} else {
		report.Migrated = append(report.Migrated, id)
	}
	return nil
}
