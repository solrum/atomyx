package runfolder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IndexSchemaVersion is the index.json schema version this package
// writes.
const IndexSchemaVersion = 1

// Index is the content of index.json, the per-entry record of a suite
// run folder. It is a contract for readers of a suite run, so a field is
// added but never removed or renamed within a schema version.
//
// Entries are in suite order and list every entry that ran, including the
// ones that ran before a cancelled suite stopped.
type Index struct {
	SchemaVersion int          `json:"schemaVersion"`
	RunID         string       `json:"runId"`
	Entries       []IndexEntry `json:"entries"`
}

// IndexEntry is one suite entry. Status is the result of its last
// attempt. Flaky marks an entry that failed and then passed, which counts
// as passed. Quarantined marks an entry matched by the quarantine list,
// whatever its status, and a quarantined entry that failed does not fail
// the suite.
type IndexEntry struct {
	Path string `json:"path"`
	// TestKey is the stable identity of the entry, derived like
	// Meta.TestKey. Absent on an index written without one.
	TestKey     string         `json:"testKey,omitempty"`
	Status      Result         `json:"status"`
	Flaky       bool           `json:"flaky,omitempty"`
	Quarantined bool           `json:"quarantined,omitempty"`
	Attempts    []IndexAttempt `json:"attempts"`
}

// IndexAttempt is one run of an entry. Number counts from 1. Result is
// passed or failed. Videos are the kept video file names relative to the
// artifacts directory of the run folder that holds them.
//
// DeviceID and WorkerRunID name where a sharded attempt ran and which
// worker run folder holds its artifacts. A suite that is not sharded
// leaves both empty.
type IndexAttempt struct {
	Number      int      `json:"number"`
	Result      Result   `json:"result"`
	Videos      []string `json:"videos,omitempty"`
	DeviceID    string   `json:"deviceId,omitempty"`
	WorkerRunID string   `json:"workerRunId,omitempty"`
}

// WriteIndex replaces index.json with entries, stamped with the schema
// version and the run id. The file is written through a temporary name
// and renamed, so a reader never sees a partial index. It works on an
// open Writer, so a suite writes it before the run is finalized, and on a
// finalized or closed Writer it returns ErrFinalized or ErrClosed.
func (w *Writer) WriteIndex(entries []IndexEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return err
	}
	if entries == nil {
		entries = []IndexEntry{}
	}
	index := Index{SchemaVersion: IndexSchemaVersion, RunID: w.meta.RunID, Entries: entries}
	if err := writeJSONAtomic(w.dir, IndexFileName, index); err != nil {
		return fmt.Errorf("writing %s: %w", IndexFileName, err)
	}
	return nil
}

// ReadIndex reads index.json from the run folder at dir. A folder with no
// index.json yields an error that satisfies errors.Is(err, fs.ErrNotExist),
// which is how a run that is not a suite looks. An index whose schemaVersion
// is outside 1 to IndexSchemaVersion is rejected, and a null entries list
// reads as empty.
func ReadIndex(dir string) (Index, error) {
	body, err := os.ReadFile(filepath.Join(dir, IndexFileName))
	if err != nil {
		return Index{}, fmt.Errorf("reading %s: %w", IndexFileName, err)
	}
	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return Index{}, fmt.Errorf("decoding %s: %w", IndexFileName, err)
	}
	if index.SchemaVersion < 1 || index.SchemaVersion > IndexSchemaVersion {
		return Index{}, fmt.Errorf("%s schemaVersion %d is outside 1 to %d", IndexFileName, index.SchemaVersion, IndexSchemaVersion)
	}
	if index.Entries == nil {
		index.Entries = []IndexEntry{}
	}
	return index, nil
}
