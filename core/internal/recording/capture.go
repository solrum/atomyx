package recording

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/runfolder"
)

// DefaultStopBound bounds the whole cleanup of one recording: stopping
// the device process, pulling the kept segments, and removing what is
// left on the device. A pull of a full three-minute segment takes several
// seconds on a slow link, so the bound leaves a wide margin above that.
const DefaultStopBound = 20 * time.Second

// Artifacts is the part of a run folder writer a recording needs.
type Artifacts interface {
	// ArtifactsDir returns the artifacts directory, creating it if needed.
	ArtifactsDir() (string, error)
	// AdoptArtifact moves a temporary file in that directory to its final
	// name without replacing an existing file.
	AdoptArtifact(tmp, name string) (string, error)
}

var _ Artifacts = (*runfolder.Writer)(nil)

// Lifecycle starts recordings for runs and attempts. A Lifecycle with no
// recorder is the off state: Begin on it makes no recorder call and every
// Capture it returns finishes with nil.
type Lifecycle struct {
	recorder  device.ScreenRecorder
	logger    logging.Logger
	stopBound time.Duration
}

// Option configures a Lifecycle.
type Option func(*Lifecycle)

// WithStopBound sets how long stopping, pulling and discarding may take
// after a run ends. The default is DefaultStopBound.
func WithStopBound(d time.Duration) Option {
	return func(l *Lifecycle) { l.stopBound = d }
}

// NewLifecycle returns a Lifecycle over recorder. A nil recorder turns
// recording off. Recording problems are logged to logger and never
// returned, because a video must not fail the run it documents.
func NewLifecycle(recorder device.ScreenRecorder, logger logging.Logger, opts ...Option) *Lifecycle {
	l := &Lifecycle{recorder: recorder, logger: logger, stopBound: DefaultStopBound}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Capture is one recording of one run or attempt.
type Capture struct {
	lifecycle *Lifecycle
	artifacts Artifacts
	base      string
	active    device.Recording

	once    sync.Once
	outcome *runfolder.Recording
}

// Begin starts a recording of desc into the artifacts directory of art.
// base names the recording and appears in every kept file name after
// runfolder.VideoNamePrefix. Begin never fails: a recorder that cannot
// start leaves a Capture that reports why when finished, and the run
// goes on without a video.
func (l *Lifecycle) Begin(ctx context.Context, desc device.Descriptor, art Artifacts, base string) *Capture {
	c := &Capture{lifecycle: l, artifacts: art, base: base}
	if l.recorder == nil {
		return c
	}
	dir, err := art.ArtifactsDir()
	if err != nil {
		c.outcome = l.failure(err, "preparing the recording directory failed")
		return c
	}
	rec, err := l.recorder.Start(ctx, desc, dir, runfolder.VideoNamePrefix+base)
	if err != nil {
		c.outcome = l.failure(err, "starting the screen recording failed")
		return c
	}
	c.active = rec
	return c
}

// Finish ends the recording for a run that ended with result and returns
// the value for the recording field of meta.json. A failed result keeps
// the video under final names. Any other result discards it and returns
// nil. A recording that could not start, or could not be kept, returns
// its unsupported or error state whatever the result. Finish is safe to
// call more than once and returns the first answer.
//
// Cleanup runs under its own bounded context that ignores any
// cancellation of the run, so a cancelled or interrupted run still stops
// the device process and removes its files.
func (c *Capture) Finish(result runfolder.Result) *runfolder.Recording {
	c.once.Do(func() { c.outcome = c.finish(result) })
	return c.outcome
}

func (c *Capture) finish(result runfolder.Result) *runfolder.Recording {
	if c.outcome != nil || c.active == nil {
		return c.outcome
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.lifecycle.stopBound)
	defer cancel()
	if result != runfolder.ResultFailed {
		c.discard(ctx)
		return nil
	}
	paths, err := c.active.Stop(ctx)
	if err != nil {
		c.discard(ctx)
		return c.lifecycle.failure(err, "stopping the screen recording failed")
	}
	files, err := c.adopt(paths)
	if err != nil {
		c.discard(ctx)
		return c.lifecycle.failure(err, "keeping the screen recording failed")
	}
	return &runfolder.Recording{State: runfolder.RecordingKept, Files: files}
}

func (c *Capture) discard(ctx context.Context) {
	if err := c.active.Discard(ctx); err != nil {
		c.lifecycle.logger.Error("discarding the screen recording failed", err, nil)
	}
}

func (c *Capture) adopt(paths []string) ([]string, error) {
	files := make([]string, 0, len(paths))
	for i, tmp := range paths {
		name := fmt.Sprintf("%s%s-%d%s", runfolder.VideoNamePrefix, c.base, i+1, filepath.Ext(tmp))
		if _, err := c.artifacts.AdoptArtifact(tmp, name); err != nil {
			return nil, err
		}
		files = append(files, name)
	}
	return files, nil
}

// failure maps a recording error to its meta state and logs the ones that
// are not the expected unsupported-device case.
func (l *Lifecycle) failure(err error, msg string) *runfolder.Recording {
	code, _, _ := diag.Describe(err)
	if code == diag.CodeRecordingUnsupported {
		return &runfolder.Recording{State: runfolder.RecordingUnsupported, Code: code}
	}
	l.logger.Error(msg, err, logging.LogContext{"code": code})
	return &runfolder.Recording{State: runfolder.RecordingError, Code: code}
}
