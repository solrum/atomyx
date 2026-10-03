package session

import (
	"context"

	"dev.atomyx/internal/runfolder"
)

// stepRingSize is how many recent step lines a hub keeps for a viewer
// that resumes after being dropped. A line is a few hundred bytes, so
// the ring stays small, and a viewer that falls further behind than this
// learns of the loss through ObservedStepGap.
const stepRingSize = 256

// stepRing holds the newest step lines in order. A line whose sequence
// number does not exceed the newest held one starts a new run, because a
// run folder numbers its lines from 1, so the ring is emptied first.
type stepRing struct {
	lines   [stepRingSize]runfolder.StepLine
	start   int
	count   int
	lastSeq int64
}

func (r *stepRing) push(line runfolder.StepLine) {
	if line.Seq <= r.lastSeq {
		r.start, r.count = 0, 0
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

// since returns the held lines with a sequence number above sinceSeq and
// reports whether the caller lost lines. Lines are lost when the oldest
// held line is not the one right after sinceSeq, or when sinceSeq is
// ahead of the newest line, which means the numbering restarted with a
// new run.
func (r *stepRing) since(sinceSeq int64) ([]runfolder.StepLine, bool) {
	if r.count == 0 {
		return nil, false
	}
	if sinceSeq > r.lastSeq {
		return r.copyFrom(0), true
	}
	gap := r.oldestSeq() > sinceSeq+1
	skip := 0
	for skip < r.count && r.lines[(r.start+skip)%stepRingSize].Seq <= sinceSeq {
		skip++
	}
	return r.copyFrom(skip), gap
}

func (r *stepRing) copyFrom(skip int) []runfolder.StepLine {
	out := make([]runfolder.StepLine, 0, r.count-skip)
	for i := skip; i < r.count; i++ {
		out = append(out, r.lines[(r.start+i)%stepRingSize])
	}
	return out
}

// resumeLocked returns the notices a subscriber that has seen every line
// up to sinceSeq starts with: a gap notice when it lost lines, then one
// step notice that covers every line it can still read.
func (r *stepRing) resumeLocked(sinceSeq int64) []Observation {
	lines, gap := r.since(sinceSeq)
	var out []Observation
	if gap {
		out = append(out, Observation{Kind: ObservedStepGap, Revision: uint64(r.oldestSeq()), Stable: true})
	}
	if len(lines) > 0 {
		out = append(out, Observation{Kind: ObservedStep, Revision: uint64(r.lastSeq), Stable: true})
	}
	return out
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

// StepsSince returns the held step lines with a sequence number above
// sinceSeq, oldest first. The flag is true when the caller lost lines,
// either because they left the ring or because a new run restarted the
// numbering below sinceSeq. A caller that has seen nothing passes zero.
func (h *Hub) StepsSince(sinceSeq int64) ([]runfolder.StepLine, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.steps.since(sinceSeq)
}

// SubscribeSince is Subscribe for a viewer that also follows step lines.
// Besides the capture notices, it receives ObservedStep for every
// published line, in order with the capture notices. A subscriber that
// resumes with the last sequence number it read starts with an
// ObservedStepGap when lines it needs left the ring, then with one
// ObservedStep covering the lines still held, which it reads with
// StepsSince. Capture notices stay payload-free, so a slow reader still
// sees only the newest tree and screenshot.
func (h *Hub) SubscribeSince(ctx context.Context, sinceSeq int64) (<-chan Observation, error) {
	return h.subscribe(ctx, &sinceSeq)
}
