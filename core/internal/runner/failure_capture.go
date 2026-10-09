package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
)

// FailureCaptureBoundMs bounds the whole evidence capture for one
// failed step: the screenshot, the UI tree, and the log together. A
// capture that is still running at the bound is abandoned, and the
// files that finished are kept.
const FailureCaptureBoundMs int64 = 5000

// StepFailure describes the top-level step that just failed.
type StepFailure struct {
	// StepNumber is the step's 1-based position in the script's own
	// step list, the same number a report prints as "Failed at step N"
	// and equal to StepResult.StepIndex plus one.
	StepNumber int
	// Command is the failed step's command name.
	Command string
	// Detail is the failed step's result detail, or nil when it
	// reported none.
	Detail *string
	// Events holds every step event the runner emitted from the start
	// of the failed step until it finished, including the events of
	// any nested step list it ran.
	Events []StepEvent
}

// FailureCapture records evidence about a failed step. The runner calls
// it once per failed top-level step, after the failure is classified as
// an ordinary one, and never for a run aborted because the device was
// lost, because a screenshot of an unreachable device cannot succeed.
//
// CaptureFailure returns normally in every case. It never changes the
// step result or the run outcome, and it must honor ctx.
type FailureCapture interface {
	CaptureFailure(ctx context.Context, failure StepFailure)
}

// EvidenceWriter persists one named evidence file into the run's
// artifacts directory. The writer owns the final file name, so a host
// that runs several scripts into one directory can keep names apart.
type EvidenceWriter interface {
	WriteEvidence(name string, data []byte) error
}

// FailureBundle is the FailureCapture that writes three files for a
// failed step through an EvidenceWriter, where NNN is the zero-padded
// StepNumber:
//
//   - step-NNN-failure.png: the screenshot. The extension follows the
//     bytes the device returned, jpg for JPEG data and png otherwise.
//   - step-NNN-tree.json: the UI hierarchy as JSON.
//   - step-NNN-log.jsonl: one JSON object per step event of the failed
//     step, followed by one note object for each artifact that could
//     not be captured and one truncation note when the bound was hit.
//
// The log file is always written, so it explains any missing artifact.
type FailureBundle struct {
	orchestra *orchestra.Orchestra
	clock     orchestra.Clock
	logger    logging.Logger
	sink      EvidenceWriter
}

// FailureBundleDeps constructs a FailureBundle. Every field is
// required.
type FailureBundleDeps struct {
	// Orchestra takes the screenshot and the UI tree.
	Orchestra *orchestra.Orchestra
	// Clock times the capture bound.
	Clock orchestra.Clock
	// Logger records evidence files that could not be written.
	Logger logging.Logger
	// Sink receives each evidence file.
	Sink EvidenceWriter
}

// NewFailureBundle constructs a FailureBundle from deps.
func NewFailureBundle(deps FailureBundleDeps) *FailureBundle {
	return &FailureBundle{orchestra: deps.Orchestra, clock: deps.Clock, logger: deps.Logger, sink: deps.Sink}
}

type captured struct {
	data []byte
	err  error
}

type logNote struct {
	Type     string `json:"type"`
	Artifact string `json:"artifact,omitempty"`
	Note     string `json:"note"`
}

type logEvent struct {
	Type       StepEventType `json:"type"`
	StepIndex  int           `json:"stepIndex"`
	Command    string        `json:"command"`
	Summary    string        `json:"summary"`
	Depth      int           `json:"depth"`
	Ok         *bool         `json:"ok,omitempty"`
	Detail     *string       `json:"detail,omitempty"`
	DurationMs int64         `json:"durationMs,omitempty"`
}

// CaptureFailure implements FailureCapture.
func (b *FailureBundle) CaptureFailure(ctx context.Context, failure StepFailure) {
	boundCtx, cancel := context.WithCancel(ctx)
	var boundHit atomic.Bool
	timerDone := make(chan struct{})
	go func() {
		defer close(timerDone)
		if b.clock.Sleep(boundCtx, FailureCaptureBoundMs) == nil {
			boundHit.Store(true)
			cancel()
		}
	}()
	defer func() {
		cancel()
		<-timerDone
	}()

	shotCh := make(chan captured, 1)
	treeCh := make(chan captured, 1)
	var calls sync.WaitGroup
	calls.Add(2)
	go func() {
		defer calls.Done()
		defer recoverInto(shotCh)
		data, err := b.orchestra.Screenshot(boundCtx)
		shotCh <- captured{data: data, err: err}
	}()
	go func() {
		defer calls.Done()
		defer recoverInto(treeCh)
		tree, err := b.orchestra.Hierarchy(boundCtx)
		if err != nil {
			treeCh <- captured{err: err}
			return
		}
		data, err := json.Marshal(orchestra.MaskSecure(tree))
		treeCh <- captured{data: data, err: err}
	}()

	var shot, tree captured
	var shotDone, treeDone bool
await:
	for !shotDone || !treeDone {
		select {
		case out := <-shotCh:
			if out.err != nil && boundCtx.Err() != nil {
				break await
			}
			shot, shotDone = out, true
		case out := <-treeCh:
			if out.err != nil && boundCtx.Err() != nil {
				break await
			}
			tree, treeDone = out, true
		case <-boundCtx.Done():
			break await
		}
	}
	// The driver serves one caller at a time, so no capture call may
	// outlive this method and overlap the next step. Cancelling the
	// context makes each call return, and the bound reaches the driver
	// through that context.
	cancel()
	calls.Wait()
	if !shotDone {
		shot, shotDone = drain(shotCh)
	}
	if !treeDone {
		tree, treeDone = drain(treeCh)
	}

	var notes []logNote
	prefix := fmt.Sprintf("step-%03d-", failure.StepNumber)
	if shotDone {
		notes = b.writeShot(prefix, shot, notes)
	}
	if treeDone {
		notes = b.writeTree(prefix, tree, notes)
	}
	if !shotDone || !treeDone {
		notes = append(notes, truncationNote(boundHit.Load(), shotDone, treeDone))
	}
	b.writeLog(prefix, failure, notes)
}

// drain takes a successful result that is already waiting in ch without
// blocking, so a capture that finished at the same instant the bound
// fired is kept. A waiting error is the cancellation the bound itself
// caused, so it counts as unfinished.
func drain(ch <-chan captured) (captured, bool) {
	select {
	case out := <-ch:
		if out.err != nil {
			return captured{}, false
		}
		return out, true
	default:
		return captured{}, false
	}
}

// recoverInto turns a panic in a capture goroutine into a failed
// result, so a misbehaving driver cannot crash the run.
func recoverInto(ch chan<- captured) {
	if rec := recover(); rec != nil {
		ch <- captured{err: fmt.Errorf("capture panicked: %v", rec)}
	}
}

func (b *FailureBundle) writeShot(prefix string, shot captured, notes []logNote) []logNote {
	if shot.err != nil {
		return append(notes, logNote{Type: "captureFailed", Artifact: "screenshot", Note: shot.err.Error()})
	}
	if len(shot.data) == 0 {
		return append(notes, logNote{Type: "captureFailed", Artifact: "screenshot", Note: "the device returned no image data"})
	}
	ext := "png"
	if http.DetectContentType(shot.data) == "image/jpeg" {
		ext = "jpg"
	}
	b.write(prefix+"failure."+ext, shot.data)
	return notes
}

func (b *FailureBundle) writeTree(prefix string, tree captured, notes []logNote) []logNote {
	if tree.err != nil {
		return append(notes, logNote{Type: "captureFailed", Artifact: "tree", Note: tree.err.Error()})
	}
	b.write(prefix+"tree.json", tree.data)
	return notes
}

func (b *FailureBundle) writeLog(prefix string, failure StepFailure, notes []logNote) {
	var out strings.Builder
	enc := json.NewEncoder(&out)
	for _, e := range failure.Events {
		line := logEvent{
			Type: e.Type, StepIndex: e.StepIndex, Command: e.Command,
			Summary: e.Summary, Depth: e.Depth,
		}
		if e.Type == StepCompleted {
			ok := e.Ok
			line.Ok, line.Detail, line.DurationMs = &ok, e.Detail, e.DurationMs
		}
		_ = enc.Encode(line)
	}
	for _, n := range notes {
		_ = enc.Encode(n)
	}
	b.write(prefix+"log.jsonl", []byte(out.String()))
}

func (b *FailureBundle) write(name string, data []byte) {
	if err := b.sink.WriteEvidence(name, data); err != nil {
		b.logger.Warn("script.failureCapture.writeFailed", logging.LogContext{"name": name, "error": err.Error()})
	}
}

func truncationNote(boundHit, shotDone, treeDone bool) logNote {
	var missing []string
	if !shotDone {
		missing = append(missing, "screenshot")
	}
	if !treeDone {
		missing = append(missing, "tree")
	}
	reason := "the run was canceled"
	if boundHit {
		reason = fmt.Sprintf("the %d ms capture bound was reached", FailureCaptureBoundMs)
	}
	return logNote{Type: "truncated", Note: reason + ", missing: " + strings.Join(missing, ", ")}
}
