package inspector

import (
	"encoding/json"
	"net/http"

	"dev.atomyx/internal/session"
)

const eventStreamContentType = "application/x-ndjson"

// Event kinds of the capture feed. The two terminal kinds are the last
// line of a stream that ends because the session said so.
const (
	eventTree       = "tree"
	eventScreenshot = "screenshot"
	eventScreenSize = "screen_size"
	eventDropped    = "dropped"
	eventClosed     = "closed"
	eventStep       = "step"
	eventStepGap    = "step_gap"
	eventUnknown    = "unknown"
)

// eventLine is one line of the feed. It announces a capture and
// carries no payload, so a client reads the value from the tree or
// screenshot route when it wants it.
type eventLine struct {
	Kind     string `json:"kind"`
	Epoch    uint64 `json:"epoch"`
	Revision uint64 `json:"revision"`
	Stable   bool   `json:"stable"`
}

// stepEventLine is one step line of a run the holder drives. It carries
// the line's run id and sequence number, so a page tells runs apart and
// resumes in order. Detail is the step's own text and is empty when the
// step has none.
type stepEventLine struct {
	Kind       string `json:"kind"`
	RunID      string `json:"runId"`
	Seq        int64  `json:"seq"`
	StepIndex  int    `json:"stepIndex"`
	Command    string `json:"command"`
	Phase      string `json:"phase"`
	Ok         bool   `json:"ok"`
	DurationMs int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

func eventKind(k session.ObservationKind) string {
	switch k {
	case session.ObservedTree:
		return eventTree
	case session.ObservedScreenshot:
		return eventScreenshot
	case session.ObservedScreenSize:
		return eventScreenSize
	case session.ObservedDropped:
		return eventDropped
	case session.ObservedClosed:
		return eventClosed
	}
	return eventUnknown
}

// eventsHandler streams the session's capture notifications as
// newline-delimited JSON, one line per notification, flushed as it is
// written, so a page reads it with fetch and a stream reader. The
// stream ends when the client disconnects, the server closes, or the
// session reports that it dropped the subscriber or closed. Those two
// reports are sent as a final line first. When the viewer also follows
// step lines, each line of a run is sent as a step event in order, and a
// step_gap event reports that lines were lost. The handler never calls
// the device.
func eventsHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		steps, followsSteps := c.(session.StepSource)
		var feed <-chan session.Observation
		var err error
		if followsSteps {
			feed, err = steps.SubscribeSince(r.Context(), "", 0)
		} else {
			feed, err = c.Subscribe(r.Context())
		}
		if err != nil {
			writeFailure(w, err)
			return
		}
		flusher := http.NewResponseController(w)
		h := w.Header()
		h.Set("Content-Type", eventStreamContentType)
		w.WriteHeader(http.StatusOK)
		if flusher.Flush() != nil {
			return
		}
		enc := json.NewEncoder(w)
		var cursor stepCursor
		for {
			select {
			case <-r.Context().Done():
				return
			case obs, open := <-feed:
				if !open {
					return
				}
				if obs.Kind == session.ObservedState {
					continue
				}
				if followsSteps && (obs.Kind == session.ObservedStep || obs.Kind == session.ObservedStepGap) {
					if cursor.write(enc, steps, obs) != nil || flusher.Flush() != nil {
						return
					}
					continue
				}
				line := eventLine{Kind: eventKind(obs.Kind), Epoch: obs.Epoch, Revision: obs.Revision, Stable: obs.Stable}
				if enc.Encode(line) != nil || flusher.Flush() != nil {
					return
				}
			}
		}
	})
}

// stepCursor remembers the last step line a stream sent, so the next
// read asks the viewer only for the lines after it.
type stepCursor struct {
	runID string
	seq   int64
	// gapReported is set after a step_gap event, so the read that
	// follows a gap notice does not report the same loss again.
	gapReported bool
}

// write sends what the step notice obs announces. A gap notice becomes
// a step_gap event. A line notice reads the lines after the cursor and
// sends each as a step event. A failed read sends nothing, because the
// next notice reads from the same cursor again. It returns the first
// error of the writer.
func (s *stepCursor) write(enc *json.Encoder, src session.StepSource, obs session.Observation) error {
	if obs.Kind == session.ObservedStepGap {
		s.gapReported = true
		return enc.Encode(eventLine{Kind: eventStepGap, Revision: obs.Revision, Stable: true})
	}
	lines, gap, err := src.StepsSince(s.runID, s.seq)
	if err != nil {
		return nil
	}
	reportGap := gap && !s.gapReported
	s.gapReported = false
	if reportGap {
		if err := enc.Encode(eventLine{Kind: eventStepGap, Revision: obs.Revision, Stable: true}); err != nil {
			return err
		}
	}
	for _, l := range lines {
		event := stepEventLine{
			Kind: eventStep, RunID: l.RunID, Seq: l.Seq, StepIndex: l.StepIndex, Command: l.Command,
			Phase: string(l.Phase), Ok: l.Ok, DurationMs: l.DurationMs,
		}
		if l.Detail != nil {
			event.Detail = *l.Detail
		}
		if err := enc.Encode(event); err != nil {
			return err
		}
		s.runID, s.seq = l.RunID, l.Seq
	}
	return nil
}
