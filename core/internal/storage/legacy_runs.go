package storage

import (
	"errors"
	"fmt"
	"strings"

	"dev.atomyx/internal/runfolder"
)

// LegacyRunReader reads the flat run records saved under RunKey and
// presents each as a runfolder.Meta. It never writes.
//
// Only a record that sits directly under the runs prefix is a legacy
// run. Anything nested deeper, such as the files of a versioned run
// folder, is not a record and is ignored.
type LegacyRunReader struct {
	store Storage
}

// NewLegacyRunReader returns a reader over the flat run records in store.
func NewLegacyRunReader(store Storage) *LegacyRunReader {
	return &LegacyRunReader{store: store}
}

// Get reads one legacy run. It reports false with a nil error when no
// record exists for id, including an id that is not a bare record name.
func (r *LegacyRunReader) Get(id string) (runfolder.Meta, bool, error) {
	if !isBareRecordName(id) {
		return runfolder.Meta{}, false, nil
	}
	var record PersistedRun
	ok, err := r.store.Load(RunKey(id), &record)
	if err != nil || !ok {
		return runfolder.Meta{}, false, err
	}
	meta, err := legacyMeta(id, record)
	if err != nil {
		return runfolder.Meta{}, false, err
	}
	return meta, true, nil
}

// List reads every legacy run. A record that cannot be read is left out
// of the runs and reported in skipped, so one damaged record never
// hides the others. A failure to enumerate the records at all is
// returned as err.
func (r *LegacyRunReader) List() (runs []runfolder.Meta, skipped []error, err error) {
	keys, err := r.store.List(strings.TrimSuffix(RunKeyPrefix, "/"))
	if err != nil {
		return nil, nil, err
	}
	for _, key := range keys {
		id := strings.TrimPrefix(key, RunKeyPrefix)
		if !isBareRecordName(id) {
			continue
		}
		meta, ok, err := r.Get(id)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("legacy run %s: %w", id, err))
			continue
		}
		if ok {
			runs = append(runs, meta)
		}
	}
	return runs, skipped, nil
}

func isBareRecordName(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`)
}

// legacyMeta maps a flat record to the meta of the run folder it would
// become. A running record never closed, so it has no completion time
// and reads as cancelled and interrupted.
func legacyMeta(recordID string, record PersistedRun) (runfolder.Meta, error) {
	result, outcome, ok := runfolder.FromLegacyStatus(string(record.Status))
	if !ok {
		return runfolder.Meta{}, errors.New("unknown status " + string(record.Status))
	}
	id := record.ID
	if id == "" {
		id = recordID
	}
	meta := runfolder.Meta{
		SchemaVersion: runfolder.SchemaVersion,
		RunID:         id,
		Kind:          runfolder.KindMCP,
		StartedAt:     record.StartedAt,
		Result:        result,
		Outcome:       outcome,
		MCP: &runfolder.MCPInfo{
			Name:        record.Name,
			Source:      record.Source,
			ActionCount: record.ActionCount,
			Summary:     record.Summary,
			DurationMs:  record.DurationMs,
		},
	}
	for _, f := range record.Findings {
		meta.MCP.Findings = append(meta.MCP.Findings, runfolder.Finding{
			ID: f.ID, Title: f.Title, Description: f.Description,
			ScreenshotPath: f.ScreenshotPath, Timestamp: f.Timestamp,
		})
	}
	if record.Status != RunStatusRunning {
		completed := record.StartedAt + record.DurationMs
		if record.FinishedAt != nil {
			completed = *record.FinishedAt
		}
		meta.CompletedAt = &completed
	}
	return meta, nil
}
