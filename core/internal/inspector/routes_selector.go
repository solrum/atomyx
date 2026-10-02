package inspector

import (
	"net/http"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

const (
	routeSelectorSuggest = "/api/v1/selector/suggest"
	routeSelectorTest    = "/api/v1/selector/test"
	routeStepYAML        = "/api/v1/step/yaml"

	codeTreeStale      = "tree_stale"
	maxReportedMatches = 100
)

// SelectorRoutes is the route group that turns the stored tree into
// selectors and script steps: it suggests selectors for a node, tests a
// selector against the tree, and renders one step as YAML. Every route is an operator action and none of them
// calls the device, so they work from the tree the page displays.
func SelectorRoutes(c session.Controller) []Route {
	return []Route{
		{Method: http.MethodPost, Path: routeSelectorSuggest, Handler: suggestHandler(c), Operator: true},
		{Method: http.MethodPost, Path: routeSelectorTest, Handler: selectorTestHandler(c), Operator: true},
		{Method: http.MethodPost, Path: routeStepYAML, Handler: stepYAMLHandler(c), Operator: true},
	}
}

// storedTree returns the newest stored tree, or writes the coded
// response for a session that has none and reports false.
func storedTree(w http.ResponseWriter, c session.Viewer) (session.Snapshot, bool) {
	snap := c.Latest()
	if !snap.HasTree {
		writeCoded(w, http.StatusNotFound, codeNoTree, "no tree has been captured yet",
			"Ask for a fresh capture, then try again.")
		return session.Snapshot{}, false
	}
	return snap, true
}

type suggestRequest struct {
	TreeRevision uint64 `json:"treeRevision"`
	// Path is the child index chain of the target node in the tree of
	// TreeRevision, empty for the root. It is required, so a client
	// that forgets it does not silently address the root.
	Path *[]int `json:"path"`
}

// suggestion is a candidate selector with whether the target node is
// obscured. SuggestSelectors checks that a selector is unique, not that
// the node can be tapped, so the verdict travels beside each candidate.
type suggestion struct {
	orchestra.SuggestedSelector
	Obscured bool `json:"obscured"`
}

type suggestResponse struct {
	Epoch        uint64       `json:"epoch"`
	TreeRevision uint64       `json:"treeRevision"`
	Candidates   []suggestion `json:"candidates"`
}

// suggestHandler suggests selectors for one node of the stored tree.
// The request names the revision of the tree its path was read from,
// and a request for any other revision is refused with a coded 409,
// because a path into a replaced tree addresses a different node.
func suggestHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req suggestRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeFailure(w, err)
			return
		}
		if req.Path == nil {
			writeFailure(w, invalidArgument("the request has no path", "Send the child index chain of the node, empty for the root."))
			return
		}
		snap, ok := storedTree(w, c)
		if !ok {
			return
		}
		if req.TreeRevision != snap.TreeRevision {
			writeCoded(w, http.StatusConflict, codeTreeStale, "the tree on screen is no longer the stored tree",
				"Reload the tree, select the node again, then ask for suggestions.")
			return
		}
		candidates, err := orchestra.SuggestSelectors(snap.Tree, *req.Path)
		if err != nil {
			writeFailure(w, invalidArgument(err.Error(), "Send the path of a node in the tree."))
			return
		}
		obscured, err := orchestra.ObscurementAt(snap.Tree, *req.Path)
		if err != nil {
			writeFailure(w, invalidArgument(err.Error(), "Send the path of a node in the tree."))
			return
		}
		out := suggestResponse{Epoch: snap.TreeEpoch, TreeRevision: snap.TreeRevision, Candidates: make([]suggestion, 0, len(candidates))}
		for _, cand := range candidates {
			out.Candidates = append(out.Candidates, suggestion{SuggestedSelector: cand, Obscured: obscured.Obscured})
		}
		writeJSON(w, http.StatusOK, out)
	})
}

type selectorRequest struct {
	Selector orchestra.Selector `json:"selector"`
}

type selectorMatch struct {
	Path   []int        `json:"path"`
	Bounds *wire.Bounds `json:"bounds,omitempty"`
}

type selectorTestResponse struct {
	Epoch        uint64          `json:"epoch"`
	TreeRevision uint64          `json:"treeRevision"`
	Count        int             `json:"count"`
	Matches      []selectorMatch `json:"matches"`
	// Truncated is true when Count exceeds the matches listed.
	Truncated bool `json:"truncated"`
}

// selectorTestHandler resolves a selector against the stored tree the
// way the runtime does, with the priority broadening and the selector
// filters of the Orchestra, and lists the matches with their paths and
// bounds in resolution order.
func selectorTestHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req selectorRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeFailure(w, err)
			return
		}
		if err := orchestra.RequireNonEmpty(req.Selector); err != nil {
			writeFailure(w, err)
			return
		}
		snap, ok := storedTree(w, c)
		if !ok {
			return
		}
		cursors := orchestra.Find(orchestra.CompileSelector(req.Selector), snap.Tree)
		out := selectorTestResponse{
			Epoch: snap.TreeEpoch, TreeRevision: snap.TreeRevision,
			Count: len(cursors), Matches: make([]selectorMatch, 0, min(len(cursors), maxReportedMatches)),
			Truncated: len(cursors) > maxReportedMatches,
		}
		for _, cur := range cursors[:min(len(cursors), maxReportedMatches)] {
			m := selectorMatch{Path: cur.Path()}
			if b, ok := orchestra.ParseBounds(cur.Node.Attributes[orchestra.AttrBounds]); ok {
				m.Bounds = &b
			}
			out.Matches = append(out.Matches, m)
		}
		writeJSON(w, http.StatusOK, out)
	})
}
