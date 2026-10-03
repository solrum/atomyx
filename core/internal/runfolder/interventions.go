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
		marker, _ := json.Marshal(map[string]any{"truncated": true, "limitBytes": w.maxInterventionBytes})
		_, err := w.interventions.Write(append(marker, '\n'))
		return err
	}
	n, err := w.interventions.Write(append(body, '\n'))
	w.interventionBytes += n
	return err
}
