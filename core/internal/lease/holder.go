package lease

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"dev.atomyx/internal/diag"
)

// Holder is the display-only metadata a lease holder writes into its
// lock file. It never drives a control decision.
type Holder struct {
	PID       int        `json:"pid"`
	Command   string     `json:"command"`
	Role      string     `json:"role"`
	RunID     string     `json:"runId,omitempty"`
	Socket    string     `json:"socket,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	IdleSince *time.Time `json:"idleSince,omitempty"`
}

// State summarizes what the holder is doing, for display.
func (h Holder) State() string {
	switch {
	case h.IdleSince != nil:
		return "idle"
	case h.RunID != "":
		return "running (run open)"
	default:
		return "running"
	}
}

// Describe renders the holder for an error message.
func (h Holder) Describe() string {
	if h.PID == 0 {
		return "unknown process"
	}
	return fmt.Sprintf("pid %d (%s), %s", h.PID, h.Command, h.State())
}

// readHolder parses the metadata in the lock file at path. The second
// result is false when the file is unreadable or holds no valid
// metadata, which is normal during the instant a new holder is writing.
func readHolder(path string) (Holder, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Holder{}, false
	}
	var h Holder
	if err := json.Unmarshal(data, &h); err != nil || h.PID == 0 {
		return Holder{}, false
	}
	return h, true
}

// HeldError reports that another process holds the lease. It carries
// the code DEVICE_HELD.
type HeldError struct {
	// Holder is the display metadata the holder wrote. It is the zero
	// value when Known is false.
	Holder Holder
	// Known reports whether Holder was read successfully.
	Known bool
	cause error
}

func newHeldError(h Holder, known bool) *HeldError {
	e := &HeldError{Holder: h, Known: known}
	e.cause = diag.NewDeviceHeld(h.Describe())
	return e
}

func (e *HeldError) Error() string { return e.cause.Error() }

// Unwrap exposes the coded diagnostic, so diag.Describe reports
// DEVICE_HELD and its remedy.
func (e *HeldError) Unwrap() error { return e.cause }
