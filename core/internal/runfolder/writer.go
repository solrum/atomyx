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

// Root returns the directory under which the Starter creates runs, as
// given to NewStarter.
func (s *Starter) Root() string { return s.root }

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

	lock, err := publishLockedFile(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("locking run folder: %w", err)
	}
	w := &Writer{dir: dir, now: now, meta: meta, lock: lock, maxLogBytes: MaxLogBytes, maxStepsBytes: MaxStepsBytes, maxInterventionBytes: MaxInterventionsBytes}
	if err := w.writeMeta(); err != nil {
		_ = lock.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return w, nil
}

// lockBuildHook, when not nil, runs inside publishLockedFile after the
// lock file is created and again after it is locked, each time before
// it is published as run.lock. It exists so a test can probe the run
// folder at those instants.
var lockBuildHook func(stage lockBuildStage, dir string)

type lockBuildStage int

const (
	lockStageCreated lockBuildStage = iota
	lockStageLocked
)

// publishLockedFile creates the lock file under a temporary name, locks
// it, and only then renames it to run.lock. The lock belongs to the
// open file, so it survives the rename, and every run.lock another
// process can open is already locked. A probe therefore never takes the
// lock of a run that is starting, and never finds an unlocked file that
// would report the run as not live.
func publishLockedFile(dir string) (*os.File, error) {
	f, err := os.CreateTemp(dir, LockFileName+".*.tmp")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	fail := func(err error) (*os.File, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	if lockBuildHook != nil {
		lockBuildHook(lockStageCreated, dir)
	}
	if err := flock.TryExclusive(f); err != nil {
		return fail(err)
	}
	if lockBuildHook != nil {
		lockBuildHook(lockStageLocked, dir)
	}
	if err := os.Rename(tmp, LockPath(dir)); err != nil {
		return fail(err)
	}
	return f, nil
}

// Writer owns one live run folder. It is safe for concurrent use.
//
// While the Writer is open, a lock on the run's lock file marks the run
// live to every process. A Writer that is closed without Finalize, or
// whose process dies, leaves a meta with no completedAt and no lock, which
// readers settle as interrupted.
type Writer struct {
	dir                  string
	now                  func() time.Time
	maxLogBytes          int
	maxStepsBytes        int
	maxInterventionBytes int

	mu        sync.Mutex
	meta      Meta
	lock      *os.File
	log       *os.File
	logBytes  int
	truncated bool
	state     writerState

	steps          *os.File
	stepsBytes     int
	stepSeq        int64
	stepsTruncated bool

	interventions          *os.File
	interventionBytes      int
	interventionsTruncated bool
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

// Update applies mutate to a copy of the live meta and replaces
// meta.json with it, so a field that changes while the run is open,
// such as a summary, reaches the disk without waiting for Finalize.
// mutate cannot change the run id, the start time, the schema version,
// or the verdict, and the run stays open. An update on a finalized or
// closed Writer returns ErrFinalized or ErrClosed. A write failure
// leaves the meta as it was.
func (w *Writer) Update(mutate func(*Meta)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpen(); err != nil {
		return err
	}
	next := w.meta
	mutate(&next)
	next.SchemaVersion, next.RunID, next.StartedAt = w.meta.SchemaVersion, w.meta.RunID, w.meta.StartedAt
	next.CompletedAt, next.Result, next.Outcome = nil, "", ""
	prev := w.meta
	w.meta = next
	if err := w.writeMeta(); err != nil {
		w.meta = prev
		return err
	}
	return nil
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
	var logErr, stepsErr, interventionsErr error
	if w.log != nil {
		logErr = w.log.Close()
		w.log = nil
	}
	if w.steps != nil {
		stepsErr = w.steps.Close()
		w.steps = nil
	}
	if w.interventions != nil {
		interventionsErr = w.interventions.Close()
		w.interventions = nil
	}
	return errors.Join(logErr, stepsErr, interventionsErr, w.lock.Close())
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

// writeMeta replaces meta.json with the Writer's meta.
func (w *Writer) writeMeta() error {
	return writeMetaFile(w.dir, w.meta)
}

// writeMetaFile replaces meta.json in dir atomically. The content is
// flushed to a temporary file in the same directory, renamed over
// meta.json, and the directory is flushed so the rename survives a
// crash.
func writeMetaFile(dir string, meta Meta) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", MetaFileName, err)
	}
	tmp, err := os.CreateTemp(dir, MetaFileName+".*.tmp")
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
	if err := os.Rename(tmpPath, filepath.Join(dir, MetaFileName)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replacing %s: %w", MetaFileName, err)
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	serr := d.Sync()
	return errors.Join(serr, d.Close())
}
