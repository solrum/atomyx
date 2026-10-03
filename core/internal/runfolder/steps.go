package runfolder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// StepLineSchemaVersion is the schemaVersion every line of steps.jsonl
	// carries.
	StepLineSchemaVersion = 1

	// MaxStepsBytes bounds steps.jsonl. Lines past the bound are dropped
	// and a single truncation marker line ends the file.
	MaxStepsBytes = 8 << 20
)

// StepPhase tells which side of a step a StepLine reports.
type StepPhase string

const (
	// StepPhaseStarted marks the line written just before a step runs.
	StepPhaseStarted StepPhase = "started"
	// StepPhaseCompleted marks the line written just after a step ran,
	// whether it succeeded or failed.
	StepPhaseCompleted StepPhase = "completed"
)

// StepLine is one line of steps.jsonl. Ok, DurationMs and Detail carry
// meaning only on a completed line. Detail never holds typed text, and
// a caller must not put any in it.
type StepLine struct {
	SchemaVersion int `json:"schemaVersion"`
	// RunID is the id of the run folder the line belongs to. AppendStep
	// assigns it, and a value the caller sets is overwritten. Sequence
	// numbers restart with every run, so a reader that follows lines
	// across runs tells them apart by RunID.
	RunID string `json:"runId"`
	// Seq numbers the lines of one run from 1 in the order they were
	// appended. AppendStep assigns it, and a value the caller sets is
	// overwritten.
	Seq int64 `json:"seq"`
	// StepIndex is the runner's run-wide step counter.
	StepIndex  int       `json:"stepIndex"`
	Command    string    `json:"command"`
	Phase      StepPhase `json:"phase"`
	Ok         bool      `json:"ok"`
	DurationMs int64     `json:"durationMs"`
	Detail     *string   `json:"detail"`
	// Artifacts lists artifact file names, relative to the artifacts
	// directory, that the step produced. It is never null.
	Artifacts []string `json:"artifacts"`
}

// AppendStep stamps line with the schema version, the run id and the next
// sequence number, appends it to steps.jsonl, and returns the stamped line with
// true. The sequence number advances only for a line that was written.
// Once the file would pass MaxStepsBytes, the line is dropped, one
// truncation marker line is written, and every later call drops its line.
// A dropped line is returned unwritten and unstamped with false and a nil
// error, so the caller must not publish it. StepsTruncated reports
// whether that happened. A write or encoding failure returns false and
// the error.
func (w *Writer) AppendStep(line StepLine) (StepLine, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return StepLine{}, false, err
	}
	line.SchemaVersion = StepLineSchemaVersion
	line.RunID = w.meta.RunID
	line.Seq = w.stepSeq + 1
	if line.Artifacts == nil {
		line.Artifacts = []string{}
	}
	if w.stepsTruncated {
		return dropped(line), false, nil
	}
	body, err := json.Marshal(line)
	if err != nil {
		return dropped(line), false, fmt.Errorf("encoding step line: %w", err)
	}
	if w.steps == nil {
		f, err := os.OpenFile(filepath.Join(w.dir, StepsFileName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
		if err != nil {
			return dropped(line), false, fmt.Errorf("opening steps file: %w", err)
		}
		w.steps = f
	}
	if w.stepsBytes+len(body)+1 > w.maxStepsBytes {
		w.stepsTruncated = true
		marker, _ := json.Marshal(map[string]any{"truncated": true, "limitBytes": w.maxStepsBytes})
		_, err := w.steps.Write(append(marker, '\n'))
		return dropped(line), false, err
	}
	n, err := w.steps.Write(append(body, '\n'))
	w.stepsBytes += n
	if err != nil {
		return dropped(line), false, err
	}
	w.stepSeq = line.Seq
	return line, true, nil
}

func dropped(line StepLine) StepLine {
	line.Seq = 0
	return line
}

// StepsTruncated reports whether AppendStep hit the size bound.
func (w *Writer) StepsTruncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stepsTruncated
}
