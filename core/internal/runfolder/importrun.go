package runfolder

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
)

// ErrImportConflict reports that a run folder with the imported run's
// id already exists and holds something other than the imported meta.
// Nothing is changed.
var ErrImportConflict = errors.New("a different run folder already has this id")

// ErrImportMismatch reports that the folder ImportRun wrote did not
// read back equal to its source. The new folder is removed and the
// source is untouched, so the failure concerns this run only.
var ErrImportMismatch = errors.New("the written run folder does not match its source")

// ImportOutcome says what ImportRun did.
type ImportOutcome int

const (
	// ImportWritten means the folder was created from the meta.
	ImportWritten ImportOutcome = iota
	// ImportExisting means a folder with an identical meta was already
	// there, so nothing was written.
	ImportExisting
)

// ImportRun creates the run folder for a finished or interrupted run
// whose meta was built from another store, keeping meta.RunID as the
// folder name. The folder is assembled under a scratch name inside the
// versioned runs directory and renamed into place, so no reader sees a
// half-made folder, and the written meta is read back and compared with
// meta before ImportRun reports success. A read-back that differs
// removes the new folder and returns an error matching
// ErrImportMismatch.
//
// A folder that was imported from the same source is not touched and
// reports ImportExisting, which lets an interrupted import run again,
// even after the folder was edited since. The source is identified by
// Meta.ImportedFrom, so a caller sets it to a value that names the
// source record. An existing folder whose meta equals meta, or equals
// it apart from ImportedFrom, reports ImportExisting as well. An
// existing folder with any other content returns ErrImportConflict and
// is left as it is. The folder is published with a no-replace rename,
// so a folder that appears after the check is never overwritten. An imported run gets no
// run.lock, so readers treat it as having no live writer. The caller
// supplies meta in the shape the Reader returns: an unfinished run
// carries no Result and no Outcome.
//
// Callers hold the migration lock, because ImportRun and a delete of
// the same id must not interleave.
func ImportRun(root string, meta Meta) (ImportOutcome, error) {
	if !ValidRunID(meta.RunID) {
		return 0, fmt.Errorf("run id %q is not a bare id", meta.RunID)
	}
	reader := NewReader(root)
	existing, found, err := reader.Get(meta.RunID)
	if err != nil {
		return 0, fmt.Errorf("%w: its meta is unreadable: %w", ErrImportConflict, err)
	}
	if found {
		if alreadyImported(existing, meta) {
			return ImportExisting, nil
		}
		return 0, ErrImportConflict
	}

	if err := os.MkdirAll(VersionedRunsDir(root), dirMode); err != nil {
		return 0, fmt.Errorf("creating runs directory: %w", err)
	}
	scratch, err := os.MkdirTemp(VersionedRunsDir(root), importTempPrefix+"*")
	if err != nil {
		return 0, fmt.Errorf("creating scratch folder: %w", err)
	}
	if err := os.Chmod(scratch, dirMode); err != nil {
		_ = os.RemoveAll(scratch)
		return 0, fmt.Errorf("setting scratch folder mode: %w", err)
	}
	if err := writeMetaFile(scratch, meta); err != nil {
		_ = os.RemoveAll(scratch)
		return 0, err
	}
	dir := RunDir(root, meta.RunID)
	if err := renameNoReplace(scratch, dir); err != nil {
		_ = os.RemoveAll(scratch)
		if errors.Is(err, fs.ErrExist) {
			return 0, ErrImportConflict
		}
		return 0, fmt.Errorf("publishing run folder: %w", err)
	}
	if err := syncDir(VersionedRunsDir(root)); err != nil {
		return 0, fmt.Errorf("flushing runs directory: %w", err)
	}

	back, ok, err := reader.Get(meta.RunID)
	if err != nil || !ok || !reflect.DeepEqual(back, meta) {
		_ = os.RemoveAll(dir)
		reason := "the folder reads back different"
		if err != nil {
			reason = err.Error()
		}
		return 0, fmt.Errorf("verifying run folder %q: %w: %s", meta.RunID, ErrImportMismatch, reason)
	}
	return ImportWritten, nil
}

func alreadyImported(existing, meta Meta) bool {
	if meta.ImportedFrom != "" && existing.ImportedFrom == meta.ImportedFrom {
		return true
	}
	if existing.ImportedFrom == "" {
		existing.ImportedFrom = meta.ImportedFrom
	}
	return reflect.DeepEqual(existing, meta)
}

// RemoveImportScratch deletes the scratch folders an interrupted
// import left in the versioned runs directory and returns how many it
// removed. The caller holds the migration lock, so no import is
// building one.
func RemoveImportScratch(root string) (int, error) {
	entries, err := os.ReadDir(VersionedRunsDir(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), importTempPrefix) {
			continue
		}
		if err := os.RemoveAll(RunDir(root, e.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
