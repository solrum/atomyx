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
// reports are sent as a final line first. The handler never calls the
// device.
func eventsHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feed, err := c.Subscribe(r.Context())
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
		for {
			select {
			case <-r.Context().Done():
				return
			case obs, open := <-feed:
				if !open {
					return
				}
				line := eventLine{Kind: eventKind(obs.Kind), Epoch: obs.Epoch, Revision: obs.Revision, Stable: obs.Stable}
				if enc.Encode(line) != nil || flusher.Flush() != nil {
					return
				}
			}
		}
	})
}
