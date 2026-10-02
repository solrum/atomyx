package inspector

import (
	"net/http"
	"strconv"

	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

const (
	routeSession    = "/api/v1/session"
	routeTree       = "/api/v1/tree"
	routeTreeFresh  = "/api/v1/tree/fresh"
	routeScreenshot = "/api/v1/screenshot"
	routeEvents     = "/api/v1/events"

	headerEpoch         = "X-Atomyx-Epoch"
	headerRevision      = "X-Atomyx-Revision"
	headerScale         = "X-Atomyx-Scale"
	headerPaired        = "X-Atomyx-Paired"
	headerHighlightable = "X-Atomyx-Highlightable"
)

// ReadRoutes is the route group that serves what the session holds:
// its info, the latest tree and screenshot, and the capture feed. It
// also serves the one operator action that refreshes them, a fresh
// capture. None of the read routes calls the device.
func ReadRoutes(c session.Controller) []Route {
	return []Route{
		{Method: http.MethodGet, Path: routeSession, Handler: sessionHandler(c)},
		{Method: http.MethodGet, Path: routeTree, Handler: treeHandler(c)},
		{Method: http.MethodGet, Path: routeScreenshot, Handler: screenshotHandler(c)},
		{Method: http.MethodGet, Path: routeEvents, Handler: eventsHandler(c)},
		{Method: http.MethodPost, Path: routeTreeFresh, Handler: freshTreeHandler(c), Operator: true},
	}
}

func sessionHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, c.Info())
	})
}

// treeResponse is the stored tree with the facts a page needs to use
// it. Revision identifies this tree, so a node path sent back with
// it is checked against the right tree. Scale and Highlightable come
// from the session, so the page never computes them.
type treeResponse struct {
	Epoch              uint64            `json:"epoch"`
	Revision           uint64            `json:"revision"`
	Tree               wire.TreeNodeWire `json:"tree"`
	ScreenshotRevision uint64            `json:"screenshotRevision"`
	Scale              float64           `json:"scale"`
	Paired             bool              `json:"paired"`
	Highlightable      bool              `json:"highlightable"`
}

func treeHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTree(w, c.Latest())
	})
}

func writeTree(w http.ResponseWriter, snap session.Snapshot) {
	if !snap.HasTree {
		writeCoded(w, http.StatusNotFound, codeNoTree, "no tree has been captured yet",
			"Ask for a fresh capture, or wait for the holder to read the screen.")
		return
	}
	writeJSON(w, http.StatusOK, treeResponse{
		Epoch:              snap.TreeEpoch,
		Revision:           snap.TreeRevision,
		Tree:               snap.Tree,
		ScreenshotRevision: snap.ScreenshotRevision,
		Scale:              snap.Scale,
		Paired:             snap.Paired,
		Highlightable:      snap.Highlightable,
	})
}

func screenshotHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := c.Latest()
		if !snap.HasScreenshot {
			writeCoded(w, http.StatusNotFound, codeNoScreenshot, "no screenshot has been captured yet",
				"Ask for a fresh capture, or wait for the holder to take one.")
			return
		}
		h := w.Header()
		h.Set("Content-Type", imageContentType(snap.ScreenshotFormat))
		h.Set(headerEpoch, strconv.FormatUint(snap.ScreenshotEpoch, 10))
		h.Set(headerRevision, strconv.FormatUint(snap.ScreenshotRevision, 10))
		h.Set(headerScale, strconv.FormatFloat(snap.Scale, 'f', -1, 64))
		h.Set(headerPaired, strconv.FormatBool(snap.Paired))
		h.Set(headerHighlightable, strconv.FormatBool(snap.Highlightable))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(snap.Screenshot)
	})
}

func imageContentType(f session.ImageFormat) string {
	switch f {
	case session.ImagePNG:
		return "image/png"
	case session.ImageJPEG:
		return "image/jpeg"
	}
	return "application/octet-stream"
}

func freshTreeHandler(c session.Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap, err := c.FreshTree(r.Context())
		if err != nil {
			writeFailure(w, err)
			return
		}
		writeTree(w, snap)
	})
}
