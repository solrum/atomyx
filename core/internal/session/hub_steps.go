package session

import (
	"context"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/runfolder"
)

// stepRingSize is how many recent step lines a hub keeps for a viewer
// that resumes after being dropped. A line is a few hundred bytes, so
// the ring stays small, and a viewer that falls further behind than this
// learns of the loss through ObservedStepGap.
const stepRingSize = 256

// stepRing holds the newest step lines of one run in order. A line that
// belongs to another run than the held ones starts a new run, because a
// run folder numbers its lines from 1, so the ring is emptied first.
type stepRing struct {
	lines   [stepRingSize]runfolder.StepLine
	start   int
	count   int
	runID   string
	lastSeq int64
}

func (r *stepRing) push(line runfolder.StepLine) {
	if line.RunID != r.runID {
		r.start, r.count = 0, 0
		r.runID = line.RunID
	}
	r.lastSeq = line.Seq
	if r.count < stepRingSize {
		r.lines[(r.start+r.count)%stepRingSize] = line
		r.count++
		return
	}
	r.lines[r.start] = line
	r.start = (r.start + 1) % stepRingSize
}

func (r *stepRing) oldestSeq() int64 {
	if r.count == 0 {
		return 0
	}
	return r.lines[r.start].Seq
}

// since returns the held lines the caller has not read and reports
// whether it lost lines. A caller that last read line sinceSeq of run
// runID has read nothing of the held run when runID is not the held run
// or is empty, so it gets every held line. Lines are lost when the
// oldest held line is not the one right after the caller's position.
// A sinceSeq beyond the newest line of the held run is an error, because
// no such line exists.
func (r *stepRing) since(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	if r.count == 0 {
		return nil, false, nil
	}
	if runID == "" || runID != r.runID {
		sinceSeq = 0
	} else if sinceSeq > r.lastSeq {
		return nil, false, diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("sinceSeq %d is beyond the newest step line %d of run %s", sinceSeq, r.lastSeq, r.runID),
			diag.InvalidArgumentRemedy, nil)
	}
	gap := r.oldestSeq() > sinceSeq+1
	skip := 0
	for skip < r.count && r.lines[(r.start+skip)%stepRingSize].Seq <= sinceSeq {
		skip++
	}
	return r.copyFrom(skip), gap, nil
}

func (r *stepRing) copyFrom(skip int) []runfolder.StepLine {
	out := make([]runfolder.StepLine, 0, r.count-skip)
	for i := skip; i < r.count; i++ {
		out = append(out, r.lines[(r.start+i)%stepRingSize])
	}
	return out
}

// resumeLocked returns the notices a subscriber that last read line
// sinceSeq of run runID starts with: a gap notice when it lost lines,
// then one step notice that covers every line it can still read.
func (r *stepRing) resumeLocked(runID string, sinceSeq int64) ([]Observation, error) {
	lines, gap, err := r.since(runID, sinceSeq)
	if err != nil {
		return nil, err
	}
	var out []Observation
	if gap {
		out = append(out, Observation{Kind: ObservedStepGap, Revision: uint64(r.oldestSeq()), Stable: true})
	}
	if len(lines) > 0 {
		out = append(out, Observation{Kind: ObservedStep, Revision: uint64(r.lastSeq), Stable: true})
	}
	return out, nil
}

func isStepNotice(k ObservationKind) bool {
	return k == ObservedStep || k == ObservedStepGap
}

// PublishStep stores a stamped step line and notifies every step
// subscriber. It never blocks on a subscriber, and one that is too far
// behind is dropped like any other.
func (h *Hub) PublishStep(line runfolder.StepLine) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps.push(line)
	h.notifyLocked(Observation{Kind: ObservedStep, Revision: uint64(line.Seq), Stable: true})
}

// StepsSince returns the held step lines the caller has not read, oldest
// first, given the run and sequence number of the last line it read. The
// flag is true when the caller lost lines because they left the ring. A
// caller that has read nothing, or that last read another run, passes an
// empty or stale runID and gets every held line of the current run. A
// sinceSeq beyond the newest line of the same run fails with
// diag.CodeInvalidArgument.
func (h *Hub) StepsSince(runID string, sinceSeq int64) ([]runfolder.StepLine, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.steps.since(runID, sinceSeq)
}

// SubscribeSince is Subscribe for a viewer that also follows step lines.
// Besides the capture notices, it receives ObservedStep for every
// published line, in order with the capture notices. A subscriber that
// resumes with the run and sequence number of the last line it read
// starts with an ObservedStepGap when lines it needs left the ring, then
// with one ObservedStep covering the lines still held, which it reads
// with StepsSince. Capture notices stay payload-free, so a slow reader
// still sees only the newest tree and screenshot. A sinceSeq beyond the
// newest line of the same run fails with diag.CodeInvalidArgument.
func (h *Hub) SubscribeSince(ctx context.Context, runID string, sinceSeq int64) (<-chan Observation, error) {
	return h.subscribe(ctx, &stepResume{runID: runID, sinceSeq: sinceSeq})
}
