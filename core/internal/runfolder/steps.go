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

// AppendStep stamps line with the schema version and the next sequence
// number, appends it to steps.jsonl, and returns the stamped line. Once
// the file would pass MaxStepsBytes, the line is dropped, one truncation
// marker line is written, and every later call drops its line and
// returns it unwritten with a nil error. StepsTruncated reports whether
// that happened. Sequence numbers keep counting across dropped lines.
func (w *Writer) AppendStep(line StepLine) (StepLine, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return StepLine{}, err
	}
	w.stepSeq++
	line.SchemaVersion = StepLineSchemaVersion
	line.Seq = w.stepSeq
	if line.Artifacts == nil {
		line.Artifacts = []string{}
	}
	if w.stepsTruncated {
		return line, nil
	}
	body, err := json.Marshal(line)
	if err != nil {
		return line, fmt.Errorf("encoding step line: %w", err)
	}
	if w.steps == nil {
		f, err := os.OpenFile(filepath.Join(w.dir, StepsFileName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
		if err != nil {
			return line, fmt.Errorf("opening steps file: %w", err)
		}
		w.steps = f
	}
	if w.stepsBytes+len(body)+1 > w.maxStepsBytes {
		w.stepsTruncated = true
		marker, _ := json.Marshal(map[string]any{"truncated": true, "limitBytes": w.maxStepsBytes})
		_, err := w.steps.Write(append(marker, '\n'))
		return line, err
	}
	n, err := w.steps.Write(append(body, '\n'))
	w.stepsBytes += n
	return line, err
}

// StepsTruncated reports whether AppendStep hit the size bound.
func (w *Writer) StepsTruncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stepsTruncated
}
