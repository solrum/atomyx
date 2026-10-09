package shard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the version every line carries. Parent and worker
// are always the same executable, so the protocol is internal and a
// mismatch means the worker is not what the parent started.
const ProtocolVersion = 1

// The worker protocol is the contract between a sharding parent and one
// worker process.
//
// The parent writes entry paths to the worker's standard input, one per
// line, and closes the input when no entry is left. It sends the next path
// only after the worker reported the previous one, so a worker holds at
// most one entry.
//
// The worker writes one JSON object per line to its standard output and
// nothing else, and every object has a version field "v" and a kind:
//
//   - ready: the worker holds its device and waits for paths. RunID names
//     the run folder the worker writes. A worker that cannot acquire its
//     device sends no ready line.
//   - entry: one entry finished. Entry describes it, with one attempt per
//     run, and DeviceLost is true when the worker lost its device during
//     or right after the entry. A worker that lost its device sends no
//     further line and exits.
//   - error: the worker cannot continue. Code is the stable error code,
//     for example DEVICE_HELD, and Message says why. The worker exits
//     right after.
//     A worker that loses its device while it runs no entry sends code
//     DeviceLostCode, and the entry it was sent, if any, never started.
//
// The worker reports human output on standard error. When its input
// reaches end of file it finishes its run folder and exits.

// DeviceLostCode is the code of the error line a worker sends when it loses
// its device while it runs no entry.
const DeviceLostCode = "DEVICE_LOST"

// LineKind names what a worker line reports.
type LineKind string

// The values of Line.Kind.
const (
	LineReady LineKind = "ready"
	LineEntry LineKind = "entry"
	LineError LineKind = "error"
)

// Line is one worker report.
type Line struct {
	V          int      `json:"v"`
	Kind       LineKind `json:"kind"`
	RunID      string   `json:"runId,omitempty"`
	Entry      *Entry   `json:"entry,omitempty"`
	DeviceLost bool     `json:"deviceLost,omitempty"`
	Code       string   `json:"code,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// Entry is the outcome of one suite entry. Status is the status of the
// last attempt, and DurationMs spans every attempt. Flaky marks an entry
// that failed and then passed, and Quarantined marks an entry on the
// quarantine list.
type Entry struct {
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Error       *string   `json:"error,omitempty"`
	DurationMs  int64     `json:"durationMs"`
	Flaky       bool      `json:"flaky,omitempty"`
	Quarantined bool      `json:"quarantined,omitempty"`
	Attempts    []Attempt `json:"attempts"`
}

// Attempt is one run of an entry. Videos are the kept file names relative
// to the artifacts directory of the worker run folder. Failure describes a
// failed attempt that ran its test, and is empty for an attempt that passed
// or could not run, in which case Error says why. A worker leaves
// DeviceID and WorkerRunID empty, and the coordinator fills them in to say
// where the attempt ran.
type Attempt struct {
	Number      int      `json:"number"`
	Status      string   `json:"status"`
	Error       *string  `json:"error,omitempty"`
	Failure     string   `json:"failure,omitempty"`
	DurationMs  int64    `json:"durationMs"`
	Videos      []string `json:"videos,omitempty"`
	DeviceID    string   `json:"deviceId,omitempty"`
	WorkerRunID string   `json:"workerRunId,omitempty"`
}

// EncodeLine writes line to w as one JSON object ended by a newline,
// stamping the protocol version.
func EncodeLine(w io.Writer, line Line) error {
	line.V = ProtocolVersion
	encoded, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("encoding a worker line: %w", err)
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
}

// DecodeLine parses one worker line. It rejects a line of another
// protocol version, a line of an unknown kind, an entry line without an
// entry, and an entry with no path, no attempt, or an unknown status.
func DecodeLine(raw []byte) (Line, error) {
	var line Line
	if err := json.Unmarshal(bytes.TrimSpace(raw), &line); err != nil {
		return Line{}, fmt.Errorf("decoding a worker line: %w", err)
	}
	if line.V != ProtocolVersion {
		return Line{}, fmt.Errorf("worker line has protocol version %d, want %d", line.V, ProtocolVersion)
	}
	switch line.Kind {
	case LineReady, LineError:
	case LineEntry:
		if line.Entry == nil {
			return Line{}, errors.New("worker entry line carries no entry")
		}
		if err := validateEntry(line.Entry); err != nil {
			return Line{}, err
		}
	default:
		return Line{}, fmt.Errorf("worker line has unknown kind %q", line.Kind)
	}
	return line, nil
}

// Statuses an entry or an attempt reports.
const (
	statusPassed = "passed"
	statusFailed = "failed"
)

// validateEntry rejects an entry the coordinator cannot merge: one without
// a path, without any attempt, or with a status other than passed or
// failed on the entry or on an attempt.
func validateEntry(e *Entry) error {
	if e.Path == "" {
		return errors.New("worker entry has no path")
	}
	if len(e.Attempts) == 0 {
		return fmt.Errorf("worker entry %s has no attempts", e.Path)
	}
	if !knownStatus(e.Status) {
		return fmt.Errorf("worker entry %s has unknown status %q", e.Path, e.Status)
	}
	for _, attempt := range e.Attempts {
		if !knownStatus(attempt.Status) {
			return fmt.Errorf("worker entry %s has an attempt with unknown status %q", e.Path, attempt.Status)
		}
	}
	return nil
}

func knownStatus(status string) bool { return status == statusPassed || status == statusFailed }
