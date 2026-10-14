package history

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"dev.atomyx/internal/runfolder"
)

const (
	benchRuns = 1000
	benchKeys = 50
)

func syntheticRecords(n int) []Record {
	records := make([]Record, n)
	for i := range records {
		outcome := OutcomePassed
		switch {
		case i%17 == 0:
			outcome = OutcomeFailed
		case i%11 == 0:
			outcome = OutcomeFlaky
		}
		records[i] = Record{
			Key:         fmt.Sprintf("flows/test-%02d.yml", i%benchKeys),
			RunID:       fmt.Sprintf("run-%05d", i),
			StartedAtMs: int64(i) * 1000,
			Outcome:     outcome,
			DurationMs:  int64(500 + i%700),
			HasDuration: true,
		}
	}
	return records
}

func BenchmarkAggregate1000(b *testing.B) {
	records := syntheticRecords(benchRuns)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		Aggregate(records, Window{})
	}
}

// readerSource adapts a runfolder.Reader to Source so the benchmark scans
// real run folders without importing the storage package.
type readerSource struct {
	root   string
	reader *runfolder.Reader
}

func (s readerSource) Metas() ([]runfolder.Meta, int, error) {
	metas, skipped, err := s.reader.List()
	return metas, len(skipped), err
}

func (s readerSource) ReadIndex(runID string) (runfolder.Index, error) {
	return runfolder.ReadIndex(runfolder.RunDir(s.root, runID))
}

func writeRunFolders(b *testing.B, root string, n int) {
	b.Helper()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		at := base.Add(time.Duration(i) * time.Second)
		starter := runfolder.NewStarter(root, func() time.Time { return at }, rand.Reader)
		w, err := starter.Start("bench", runfolder.Meta{
			Kind:    runfolder.KindScript,
			TestKey: fmt.Sprintf("flows/test-%02d.yml", i%benchKeys),
		})
		if err != nil {
			b.Fatal(err)
		}
		outcome := runfolder.OutcomePassed
		if i%17 == 0 {
			outcome = runfolder.OutcomeFailed
		}
		if err := w.Finalize(outcome, nil); err != nil {
			b.Fatal(err)
		}
		if err := w.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanAndAggregate1000Folders(b *testing.B) {
	root := b.TempDir()
	writeRunFolders(b, root, benchRuns)
	source := readerSource{root: root, reader: runfolder.NewReader(root)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		records, err := Collect(source)
		if err != nil {
			b.Fatal(err)
		}
		if len(records) != benchRuns {
			b.Fatalf("collected %d records, want %d", len(records), benchRuns)
		}
		Aggregate(records, Window{})
	}
}
