package runfolder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MaxInterventionsBytes bounds interventions.jsonl. Lines past the bound
// are dropped and a single truncation marker line ends the file.
const MaxInterventionsBytes = 1 << 20

// InterventionLine is one line of interventions.jsonl: something a person
// or another process did to the device holder while the run was open.
// AtMs is the time in epoch milliseconds, and Client names the process
// that acted.
type InterventionLine struct {
	Kind   string `json:"kind"`
	AtMs   int64  `json:"atMs"`
	Client string `json:"client"`

	// The fields below describe a humanAction line and are empty for the
	// other kinds. Step is the step as the person wrote it, with the text
	// of a type step always masked.
	Step       string   `json:"step,omitempty"`
	Command    string   `json:"command,omitempty"`
	Outcome    string   `json:"outcome,omitempty"`
	Message    string   `json:"message,omitempty"`
	DurationMs int64    `json:"durationMs,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

// InterventionsTruncated is the last line of an interventions.jsonl that
// reached its size bound.
type InterventionsTruncated struct {
	Truncated  bool `json:"truncated"`
	LimitBytes int  `json:"limitBytes"`
}

// InterventionsDropped reports how many lines AppendIntervention discarded
// because interventions.jsonl reached its bound. Finalize records a
// non-zero count in meta.json.
func (w *Writer) InterventionsDropped() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.interventionsDropped
}

// AppendIntervention appends line to interventions.jsonl. Once the file
// would pass MaxInterventionsBytes, the line is dropped, one truncation
// marker line is written, and every later call drops its line and returns
// nil.
func (w *Writer) AppendIntervention(line InterventionLine) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return err
	}
	if w.interventionsTruncated {
		w.interventionsDropped++
		return nil
	}
	body, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("encoding intervention line: %w", err)
	}
	if w.interventions == nil {
		f, err := os.OpenFile(filepath.Join(w.dir, InterventionsFileName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
		if err != nil {
			return fmt.Errorf("opening interventions file: %w", err)
		}
		w.interventions = f
	}
	if w.interventionBytes+len(body)+1 > w.maxInterventionBytes {
		w.interventionsTruncated = true
		w.interventionsDropped++
		marker, _ := json.Marshal(InterventionsTruncated{Truncated: true, LimitBytes: w.maxInterventionBytes})
		_, err := w.interventions.Write(append(marker, '\n'))
		return err
	}
	n, err := w.interventions.Write(append(body, '\n'))
	w.interventionBytes += n
	return err
}
