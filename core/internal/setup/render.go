package setup

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON renders {"ok": bool, "steps": [{id, status, message,
// remedy}]} followed by a newline. Steps is always an array.
func (r Report) WriteJSON(w io.Writer) error {
	steps := r.Steps
	if steps == nil {
		steps = []StepReport{}
	}
	return json.NewEncoder(w).Encode(struct {
		OK    bool         `json:"ok"`
		Steps []StepReport `json:"steps"`
	}{OK: r.OK(), Steps: steps})
}

// WriteText renders the report for a person: one line per step with
// its status and message, the remedy on the line below when there is
// one, and a closing verdict.
func (r Report) WriteText(w io.Writer) {
	for _, s := range r.Steps {
		_, _ = fmt.Fprintf(w, "  %-7s %s: %s\n", s.Status, s.ID, s.Message)
		if s.Remedy != "" {
			_, _ = fmt.Fprintf(w, "          remedy: %s\n", s.Remedy)
		}
	}
	if r.OK() {
		_, _ = fmt.Fprintln(w, "Setup is complete.")
		return
	}
	_, _ = fmt.Fprintln(w, "Setup is not complete. Apply the remedies above, then run it again.")
}
