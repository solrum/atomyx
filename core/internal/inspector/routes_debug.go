package inspector

import (
	"net/http"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

const (
	routeDebugPause       = "/api/v1/debug/pause"
	routeDebugResume      = "/api/v1/debug/resume"
	routeDebugStep        = "/api/v1/debug/step"
	routeDebugBreakpoints = "/api/v1/debug/breakpoints"
	routeDebugVariables   = "/api/v1/debug/variables"
	routeAct              = "/api/v1/act"

	// maxBreakpoints bounds the replace-all list a request may carry.
	maxBreakpoints = 256
)

// DebugSource is implemented by a Source that can reach the debug
// controls of the holder it shows. Debug reports false when the holder
// does not advertise the debug feature.
type DebugSource interface {
	Debug() (session.DebugClient, bool)
}

// Debug returns the debug client of the served target, or false while no
// device is served or the target's holder offers no debugging.
func (h *Host) Debug() (session.DebugClient, bool) {
	held, ok := h.held()
	if !ok {
		return nil, false
	}
	src, ok := held.(DebugSource)
	if !ok {
		return nil, false
	}
	return src.Debug()
}

// DebugRoutes is the route group that forwards the operator's debug
// requests to the holder of the watched run: pause, resume and step,
// the breakpoint list, the paused variables, and one step to run while
// paused. The routes only forward through the holder's debug client and
// never run a step themselves. A request is refused unless the holder
// advertises debugging, and every request except pause and the
// breakpoint list is also refused unless the run is paused.
func DebugRoutes(c Source) []Route {
	return []Route{
		{Method: http.MethodPost, Path: routeDebugPause, Handler: debugHandler(c, false, pauseHandler), Operator: true},
		{Method: http.MethodPost, Path: routeDebugResume, Handler: debugHandler(c, true, resumeHandler(false)), Operator: true},
		{Method: http.MethodPost, Path: routeDebugStep, Handler: debugHandler(c, true, resumeHandler(true)), Operator: true},
		{Method: http.MethodPost, Path: routeDebugBreakpoints, Handler: debugHandler(c, false, breakpointsHandler), Operator: true},
		{Method: http.MethodGet, Path: routeDebugVariables, Handler: debugHandler(c, true, variablesHandler), Operator: true},
		{Method: http.MethodPost, Path: routeAct, Handler: debugHandler(c, true, actHandler), Operator: true},
	}
}

type debugAction func(w http.ResponseWriter, r *http.Request, dc session.DebugClient)

// debugHandler runs action after the refusals every debug route shares.
func debugHandler(c Source, needPaused bool, action debugAction) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var dc session.DebugClient
		if src, ok := c.(DebugSource); ok {
			dc, _ = src.Debug()
		}
		if dc == nil {
			writeFailure(w, diag.New(diag.CodeUnsupported, "the process holding the device does not offer debugging",
				"Run the script with debugging enabled, or attach to a process that supports it.", nil))
			return
		}
		if needPaused && c.Info().State != session.StatePaused {
			writeFailure(w, diag.NewHolderBusy("the run is not paused"))
			return
		}
		action(w, r, dc)
	})
}

func pauseHandler(w http.ResponseWriter, r *http.Request, dc session.DebugClient) {
	if err := dc.Pause(r.Context()); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

type resumeRequest struct {
	// Force skips the advisory check of the next step, as the page asks
	// after a miss reason was shown.
	Force bool `json:"force"`
}

func resumeHandler(step bool) debugAction {
	return func(w http.ResponseWriter, r *http.Request, dc session.DebugClient) {
		var req resumeRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeFailure(w, err)
			return
		}
		out, err := dc.Resume(r.Context(), session.ResumeOptions{Step: step, Force: req.Force})
		if err != nil {
			writeFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type breakpointsRequest struct {
	// Locations replaces the whole list. It is required, so an omitted
	// field never clears the breakpoints by accident.
	Locations *[]string `json:"locations"`
}

func breakpointsHandler(w http.ResponseWriter, r *http.Request, dc session.DebugClient) {
	var req breakpointsRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	if req.Locations == nil {
		writeFailure(w, invalidArgument("the request has no locations", "Send the full list of locations, empty to clear every breakpoint."))
		return
	}
	if len(*req.Locations) > maxBreakpoints {
		writeFailure(w, invalidArgument("the breakpoint list is too long", "Send at most 256 locations."))
		return
	}
	if err := dc.SetBreakpoints(r.Context(), *req.Locations); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"locations": *req.Locations})
}

func variablesHandler(w http.ResponseWriter, r *http.Request, dc session.DebugClient) {
	out, err := dc.Variables(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type actRequest struct {
	Step string `json:"step"`
}

func actHandler(w http.ResponseWriter, r *http.Request, dc session.DebugClient) {
	var req actRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeFailure(w, err)
		return
	}
	if req.Step == "" {
		writeFailure(w, invalidArgument("the request has no step", "Send one step written in script YAML."))
		return
	}
	out, err := dc.Act(r.Context(), req.Step)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
