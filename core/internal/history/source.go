package history

import (
	"errors"
	"fmt"
	"io/fs"

	"dev.atomyx/internal/runfolder"
)

// Source supplies the recorded runs that history reads.
//
// Metas returns the meta of every readable run. ReadIndex returns the
// index.json of one run, and its error satisfies errors.Is(err,
// fs.ErrNotExist) when the run has none.
type Source interface {
	Metas() ([]runfolder.Meta, error)
	ReadIndex(runID string) (runfolder.Index, error)
}

// Collect turns the runs in source into one Record per finished test run.
//
// Rules:
//
//   - A run that has not completed, or that ended cancelled, device lost,
//     or interrupted, says nothing about the test and yields no record.
//   - A suite run with an index contributes one record per index entry
//     and none for its own meta, so a suite is never counted twice.
//   - A worker run whose sharding parent is also present contributes
//     nothing, because the parent index already holds its entries.
//   - A suite run without an index, such as a single scenario run, is
//     keyed by its own meta.
//   - A run with no test key is keyed by its script path, and a run with
//     neither is skipped.
func Collect(source Source) ([]Record, error) {
	metas, err := source.Metas()
	if err != nil {
		return nil, fmt.Errorf("listing runs: %w", err)
	}
	present := make(map[string]bool, len(metas))
	for _, m := range metas {
		present[m.RunID] = true
	}
	var records []Record
	for _, m := range metas {
		if m.CompletedAt == nil || m.ParentRunID != "" && present[m.ParentRunID] {
			continue
		}
		if m.Kind == runfolder.KindSuite {
			entries, found, err := suiteRecords(source, m)
			if err != nil {
				return nil, err
			}
			if found {
				records = append(records, entries...)
				continue
			}
		}
		if record, ok := runRecord(m); ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func suiteRecords(source Source, m runfolder.Meta) ([]Record, bool, error) {
	index, err := source.ReadIndex(m.RunID)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading the index of run %s: %w", m.RunID, err)
	}
	records := make([]Record, 0, len(index.Entries))
	for _, entry := range index.Entries {
		outcome, ok := entryOutcome(entry)
		if !ok {
			continue
		}
		records = append(records, Record{
			Key:         keyOf(entry.TestKey, entry.Path),
			RunID:       m.RunID,
			StartedAtMs: m.StartedAt,
			Outcome:     outcome,
		})
	}
	return records, true, nil
}

func entryOutcome(entry runfolder.IndexEntry) (Outcome, bool) {
	switch entry.Status {
	case runfolder.ResultPassed:
		if entry.Flaky {
			return OutcomeFlaky, true
		}
		return OutcomePassed, true
	case runfolder.ResultFailed:
		return OutcomeFailed, true
	}
	return "", false
}

func runRecord(m runfolder.Meta) (Record, bool) {
	key := keyOf(m.TestKey, m.ScriptPath)
	if key == "" {
		return Record{}, false
	}
	var outcome Outcome
	switch m.Result {
	case runfolder.ResultPassed:
		outcome = OutcomePassed
	case runfolder.ResultFailed:
		outcome = OutcomeFailed
	default:
		return Record{}, false
	}
	return Record{
		Key:         key,
		RunID:       m.RunID,
		StartedAtMs: m.StartedAt,
		Outcome:     outcome,
		DurationMs:  *m.CompletedAt - m.StartedAt,
		HasDuration: true,
	}, true
}

func keyOf(testKey, fallback string) string {
	if testKey != "" {
		return testKey
	}
	return fallback
}
