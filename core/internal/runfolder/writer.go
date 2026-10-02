package runfolder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/flock"
)

const (
	// MaxLogBytes bounds log.jsonl. Lines past the bound are dropped
	// and a single truncation marker line ends the file.
	MaxLogBytes = 8 << 20

	dirMode  = 0o755
	fileMode = 0o644
	lockMode = 0o600
)

// ErrFinalized reports use of a Writer after Finalize.
var ErrFinalized = errors.New("run folder is already finalized")

// ErrClosed reports use of a Writer after Close.
var ErrClosed = errors.New("run folder writer is closed")

// ErrIDExhausted reports that every id drawn for a run collided with
// an existing folder.
var ErrIDExhausted = errors.New("could not find an unused run id")

// Starter creates run folders under one root.
type Starter struct {
	root string
	now  func() time.Time
	rnd  io.Reader
}

// NewStarter returns a Starter that stamps runs with now and draws id
// suffixes from rnd.
func NewStarter(root string, now func() time.Time, rnd io.Reader) *Starter {
	return &Starter{root: root, now: now, rnd: rnd}
}

// Start creates the folder of a new run and returns its Writer. The
// folder is made with an exclusive mkdir, so two runs never share one.
// When the drawn id already exists, a new suffix is drawn, up to
// MaxIDAttempts times, and then Start returns ErrIDExhausted. Parent
// directories are created as needed.
//
// base supplies the descriptive fields of the meta. Start fills in
// SchemaVersion, RunID, and StartedAt, and clears every field that only
// a finished run carries. The Writer holds an exclusive lock on the
// run's lock file until Finalize or Close, and meta.json is on disk
// before Start returns.
func (s *Starter) Start(label string, base Meta) (*Writer, error) {
	if err := os.MkdirAll(VersionedRunsDir(s.root), dirMode); err != nil {
		return nil, fmt.Errorf("creating runs directory: %w", err)
	}
	at := s.now()
	for attempt := 0; attempt < MaxIDAttempts; attempt++ {
		id, err := newRunID(at, label, s.rnd)
		if err != nil {
			return nil, err
		}
		dir := RunDir(s.root, id)
		err = os.Mkdir(dir, dirMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("creating run folder: %w", err)
		}
		return openWriter(dir, id, at, s.now, base)
	}
	return nil, fmt.Errorf("%w after %d attempts", ErrIDExhausted, MaxIDAttempts)
}

func openWriter(dir, id string, at time.Time, now func() time.Time, base Meta) (*Writer, error) {
	meta := base
	meta.SchemaVersion = SchemaVersion
	meta.RunID = id
	meta.StartedAt = at.UnixMilli()
	meta.CompletedAt, meta.Result, meta.Outcome = nil, "", ""

	lock, err := os.OpenFile(LockPath(dir), os.O_RDWR|os.O_CREATE|os.O_EXCL, lockMode)
	if err == nil {
		err = flock.TryExclusive(lock)
	}
	if err != nil {
		if lock != nil {
			_ = lock.Close()
		}
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("locking run folder: %w", err)
	}
	w := &Writer{dir: dir, now: now, meta: meta, lock: lock, maxLogBytes: MaxLogBytes}
	if err := w.writeMeta(); err != nil {
		_ = lock.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return w, nil
}

// Writer owns one live run folder. It is safe for concurrent use.
//
// While the Writer is open, a lock on the run's lock file marks the run
// live to every process. A Writer that is closed without Finalize, or
// whose process dies, leaves a meta with no completedAt and no lock, which
// readers settle as interrupted.
type Writer struct {
	dir         string
	now         func() time.Time
	maxLogBytes int

	mu        sync.Mutex
	meta      Meta
	lock      *os.File
	log       *os.File
	logBytes  int
	truncated bool
	state     writerState
}

type writerState int

const (
	stateOpen writerState = iota
	stateFinalized
	stateClosed
)

// ID returns the run id.
func (w *Writer) ID() string { return w.meta.RunID }

// Dir returns the run folder path.
func (w *Writer) Dir() string { return w.dir }

// ArtifactsDir creates the run's artifacts directory if needed and
// returns its path.
func (w *Writer) ArtifactsDir() (string, error) {
	dir := ArtifactsDir(w.dir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("creating artifacts directory: %w", err)
	}
	return dir, nil
}

// Finalize records the end of the run. The end time is the Starter clock. The result derives from outcome
// through ResultOf, and an outcome outside the enum is an error that
// leaves the run open. mutate, when not nil, sets the remaining
// end-of-run fields such as FailedAtStep and TotalSteps on a copy of
// the meta. It cannot change the run id, the start time, the schema
// version, or the verdict. The completed meta replaces meta.json
// atomically, then the lock is released.
//
// Finalize on a finalized Writer returns ErrFinalized and changes
// nothing. A write failure leaves the Writer open so the caller can
// retry or Close.
func (w *Writer) Finalize(outcome Outcome, mutate func(*Meta)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return err
	}
	result, ok := ResultOf(outcome)
	if !ok {
		return fmt.Errorf("unknown outcome %q", outcome)
	}
	next := w.meta
	if mutate != nil {
		mutate(&next)
	}
	done := w.now().UnixMilli()
	next.SchemaVersion, next.RunID, next.StartedAt = w.meta.SchemaVersion, w.meta.RunID, w.meta.StartedAt
	next.CompletedAt, next.Result, next.Outcome = &done, result, outcome
	prev := w.meta
	w.meta = next
	if err := w.writeMeta(); err != nil {
		w.meta = prev
		return err
	}
	w.state = stateFinalized
	return w.releaseLocked()
}

// Close releases the run's lock without recording an outcome, which
// leaves the run to read as interrupted. Closing a finalized or closed
// Writer is a no-op.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != stateOpen {
		return nil
	}
	w.state = stateClosed
	return w.releaseLocked()
}

func (w *Writer) releaseLocked() error {
	var logErr error
	if w.log != nil {
		logErr = w.log.Close()
		w.log = nil
	}
	return errors.Join(logErr, w.lock.Close())
}

func (w *Writer) checkOpen() error {
	switch w.state {
	case stateFinalized:
		return ErrFinalized
	case stateClosed:
		return ErrClosed
	}
	return nil
}

// AppendLog appends one JSON line to log.jsonl. line is a single JSON
// value with no newline. Once the file would pass maxLogBytes, the
// line is dropped, one truncation marker line is written, and every
// later call drops its line and returns nil. LogTruncated reports
// whether that happened.
func (w *Writer) AppendLog(line []byte) error {
	if bytes.IndexByte(line, '\n') >= 0 {
		return errors.New("log line contains a newline")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return err
	}
	if w.truncated {
		return nil
	}
	if w.log == nil {
		f, err := os.OpenFile(filepath.Join(w.dir, LogFileName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
		if err != nil {
			return fmt.Errorf("opening log: %w", err)
		}
		w.log = f
	}
	if w.logBytes+len(line)+1 > w.maxLogBytes {
		w.truncated = true
		marker, _ := json.Marshal(map[string]any{"truncated": true, "limitBytes": w.maxLogBytes})
		_, err := w.log.Write(append(marker, '\n'))
		return err
	}
	n, err := w.log.Write(append(line[:len(line):len(line)], '\n'))
	w.logBytes += n
	return err
}

// LogTruncated reports whether AppendLog hit the size bound.
func (w *Writer) LogTruncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

// writeMeta replaces meta.json atomically. The content is flushed to a
// temporary file in the same directory, renamed over meta.json, and
// the directory is flushed so the rename survives a crash.
func (w *Writer) writeMeta() error {
	body, err := json.MarshalIndent(w.meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", MetaFileName, err)
	}
	tmp, err := os.CreateTemp(w.dir, MetaFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary %s: %w", MetaFileName, err)
	}
	tmpPath := tmp.Name()
	_, werr := tmp.Write(append(body, '\n'))
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temporary %s: %w", MetaFileName, err)
	}
	if err := os.Rename(tmpPath, filepath.Join(w.dir, MetaFileName)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replacing %s: %w", MetaFileName, err)
	}
	return syncDir(w.dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	serr := d.Sync()
	return errors.Join(serr, d.Close())
}
