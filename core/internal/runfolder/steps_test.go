package runfolder

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readStepFile(t *testing.T, w *Writer) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(w.Dir(), StepsFileName))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
}

func TestAppendStepAssignsMonotonicSeqAndStampsTheSchemaVersion(t *testing.T) {
	w := startOne(t, t.TempDir())
	for i := 0; i < 3; i++ {
		got, appended, err := w.AppendStep(StepLine{Seq: 99, SchemaVersion: 7, Command: "tap", Phase: StepPhaseStarted})
		if err != nil || !appended {
			t.Fatalf("append = %v, %v", appended, err)
		}
		if got.Seq != int64(i+1) || got.SchemaVersion != StepLineSchemaVersion {
			t.Fatalf("line %d stamped seq %d schemaVersion %d", i, got.Seq, got.SchemaVersion)
		}
	}
	lines := readStepFile(t, w)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	var last StepLine
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil || last.Seq != 3 {
		t.Fatalf("last line = %q, %v", lines[2], err)
	}
	if !strings.Contains(lines[0], `"artifacts":[]`) {
		t.Errorf("artifacts must encode as an empty array: %s", lines[0])
	}
}

func TestAppendStepNumbersConcurrentLinesWithoutGaps(t *testing.T) {
	w := startOne(t, t.TempDir())
	const writers, perWriter = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if _, _, err := w.AppendStep(StepLine{Command: "tap", Phase: StepPhaseCompleted}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	for i, raw := range readStepFile(t, w) {
		var line StepLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil || line.Seq != int64(i+1) {
			t.Fatalf("file line %d has seq %d (%v), want %d", i, line.Seq, err, i+1)
		}
	}
}

func TestAppendStepStopsAtTheBoundAndRecordsOneMarker(t *testing.T) {
	w := startOne(t, t.TempDir())
	w.maxStepsBytes = 300
	for i := 0; i < 6; i++ {
		if _, _, err := w.AppendStep(StepLine{Command: "tap", Phase: StepPhaseStarted}); err != nil {
			t.Fatal(err)
		}
	}
	if !w.StepsTruncated() {
		t.Fatal("bound was not reported")
	}
	lines := readStepFile(t, w)
	var marker struct {
		Truncated  bool `json:"truncated"`
		LimitBytes int  `json:"limitBytes"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &marker); err != nil || !marker.Truncated || marker.LimitBytes != 300 {
		t.Fatalf("last line = %q, %v", lines[len(lines)-1], err)
	}
	got, appended, err := w.AppendStep(StepLine{Command: "tap"})
	if err != nil || appended || got.Seq != 0 {
		t.Fatalf("append after the bound = seq %d, appended %v, err %v, want a dropped line and no error", got.Seq, appended, err)
	}
	if after := readStepFile(t, w); len(after) != len(lines) {
		t.Fatalf("file grew past the marker: %d lines, was %d", len(after), len(lines))
	}
}

func TestAppendStepAfterFinalizeReportsErrFinalized(t *testing.T) {
	w := startOne(t, t.TempDir())
	if err := w.Finalize(OutcomePassed, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.AppendStep(StepLine{}); !errors.Is(err, ErrFinalized) {
		t.Fatalf("err = %v, want ErrFinalized", err)
	}
}

func TestAppendStepReportsTheLineThatCrossesTheBoundAsNotAppended(t *testing.T) {
	w := startOne(t, t.TempDir())
	w.maxStepsBytes = 1
	got, appended, err := w.AppendStep(StepLine{Command: "tap"})
	if err != nil || appended || got.Seq != 0 {
		t.Fatalf("append = seq %d, appended %v, err %v, want a dropped line", got.Seq, appended, err)
	}
}

func TestAppendStepDoesNotAdvanceTheSequenceWhenTheFileCannotOpen(t *testing.T) {
	w := startOne(t, t.TempDir())
	blocker := filepath.Join(w.Dir(), StepsFileName)
	if err := os.Mkdir(blocker, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, appended, err := w.AppendStep(StepLine{Command: "tap"}); err == nil || appended {
		t.Fatalf("append = %v, %v, want an open failure", appended, err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	got, appended, err := w.AppendStep(StepLine{Command: "tap"})
	if err != nil || !appended || got.Seq != 1 {
		t.Fatalf("append after recovery = seq %d, appended %v, err %v, want seq 1", got.Seq, appended, err)
	}
}

func TestAppendStepStampsTheRunIDOfTheFolder(t *testing.T) {
	w := startOne(t, t.TempDir())
	got, _, err := w.AppendStep(StepLine{RunID: "caller-set", Command: "tap"})
	if err != nil || got.RunID != w.ID() || got.RunID == "" {
		t.Fatalf("stamped run id = %q, err %v, want %q", got.RunID, err, w.ID())
	}
	if lines := readStepFile(t, w); !strings.Contains(lines[0], `"runId":"`+w.ID()+`"`) {
		t.Errorf("steps file line lacks the run id: %s", lines[0])
	}
}

func TestStepLineAttemptIsOmittedForTheFirstAttempt(t *testing.T) {
	first, err := json.Marshal(StepLine{Command: "tap", Phase: StepPhaseStarted})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), "attempt") {
		t.Errorf("a first-attempt line carries an attempt field: %s", first)
	}
	retried, err := json.Marshal(StepLine{Command: "tap", Phase: StepPhaseStarted, Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(retried), `"attempt":2`) {
		t.Errorf("a retried line misses its attempt: %s", retried)
	}
}
