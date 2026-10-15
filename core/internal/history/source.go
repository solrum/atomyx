package history

import (
	"errors"
	"fmt"
	"io/fs"

	"dev.atomyx/internal/runfolder"
)

// Source supplies the recorded runs that history reads.
//
// Metas returns the meta of every readable run and the number of run
// records that could not be read and are therefore absent from that list.
// ReadIndex returns the index.json of one run, and its error satisfies
// errors.Is(err, fs.ErrNotExist) when the run has none.
type Source interface {
	Metas() (metas []runfolder.Meta, skipped int, err error)
	ReadIndex(runID string) (runfolder.Index, error)
}

// Collect turns the runs in source into one Record per finished test run.
//
// Rules:
//
//   - A run that has not completed or that ended cancelled says nothing
//     about the test and yields no record, and so does a suite entry of
//     such a run. A cancelled run is an operator decision, not a fault.
//   - A run that ended device lost or interrupted yields one aborted
//     record under its own key, with no verdict, even for a suite run. It
//     counts as an infrastructure failure and nothing else.
//   - A suite run with an index contributes one record per index entry
//     and none for its own meta, so a suite is never counted twice.
//   - A worker run whose sharding parent is also present contributes
//     nothing, because the parent index already holds its entries.
//   - A suite run without an index, such as a single scenario run, is
//     keyed by its own meta.
//   - An index entry with no attempts was never run, for example a shard
//     entry a worker did not reach, and yields no record.
//   - A run with no test key is keyed by its script path, and a run with
//     neither is skipped.
func Collect(source Source) ([]Record, error) {
	records, _, err := collect(source)
	return records, err
}

// collect is Collect that also reports how many run records the source
// could not read.
func collect(source Source) ([]Record, int, error) {
	metas, skipped, err := source.Metas()
	if err != nil {
		return nil, 0, fmt.Errorf("listing runs: %w", err)
	}
	present := make(map[string]bool, len(metas))
	for _, m := range metas {
		present[m.RunID] = true
	}
	var records []Record
	for _, m := range metas {
		if m.ParentRunID != "" && present[m.ParentRunID] {
			continue
		}
		if m.Outcome == runfolder.OutcomeDeviceLost || m.Outcome == runfolder.OutcomeInterrupted {
			if key := keyOf(m.TestKey, m.ScriptPath); key != "" {
				records = append(records, Record{Key: key, RunID: m.RunID, StartedAtMs: m.StartedAt, Aborted: true, Class: ClassInfra})
			}
			continue
		}
		if m.CompletedAt == nil {
			continue
		}
		if m.Result != runfolder.ResultPassed && m.Result != runfolder.ResultFailed {
			continue
		}
		if m.Kind == runfolder.KindSuite {
			entries, found, err := suiteRecords(source, m)
			if err != nil {
				return nil, 0, err
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
	return records, skipped, nil
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
	for i, entry := range index.Entries {
		outcome, ok := entryOutcome(entry)
		if !ok {
			continue
		}
		records = append(records, Record{
			Key:         keyOf(entry.TestKey, entry.Path),
			RunID:       m.RunID,
			StartedAtMs: m.StartedAt,
			Outcome:     outcome,
			EntryIndex:  i,
		})
	}
	return records, true, nil
}

func entryOutcome(entry runfolder.IndexEntry) (Outcome, bool) {
	if len(entry.Attempts) == 0 {
		return "", false
	}
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
	var class Class
	switch m.Result {
	case runfolder.ResultPassed:
		outcome = OutcomePassed
	case runfolder.ResultFailed:
		outcome = OutcomeFailed
		class, _ = Classify(m.Outcome, m.ErrorCode)
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
		Class:       class,
	}, true
}

func keyOf(testKey, fallback string) string {
	if testKey != "" {
		return testKey
	}
	return fallback
}
