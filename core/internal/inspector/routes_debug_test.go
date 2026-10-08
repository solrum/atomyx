package inspector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

// fakeDebugClient records what the routes forward and answers with the
// values a test sets.
type fakeDebugClient struct {
	mu          sync.Mutex
	calls       []string
	breakpoints []string
	resumeOpts  session.ResumeOptions
	actStep     string
	resumeOut   session.ResumeResult
	err         error
}

func (f *fakeDebugClient) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeDebugClient) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDebugClient) SetBreakpoints(_ context.Context, locations []string) error {
	f.record("breakpoints")
	f.breakpoints = locations
	return f.err
}

func (f *fakeDebugClient) Pause(context.Context) error {
	f.record("pause")
	return f.err
}

func (f *fakeDebugClient) Resume(_ context.Context, opts session.ResumeOptions) (session.ResumeResult, error) {
	f.record("resume")
	f.resumeOpts = opts
	return f.resumeOut, f.err
}

func (f *fakeDebugClient) Act(_ context.Context, step string) (session.ActResult, error) {
	f.record("act")
	f.actStep = step
	return session.ActResult{Command: "tap", Outcome: "passed", DurationMs: 12}, f.err
}

func (f *fakeDebugClient) Variables(context.Context) (session.VariablesView, error) {
	f.record("variables")
	return session.VariablesView{Variables: map[string]string{"user": "ann", "password": "****"}}, f.err
}

func debugHarness(t *testing.T, state session.HolderState, dc *fakeDebugClient) *apiHarness {
	t.Helper()
	ctrl := &stubController{}
	ctrl.info.State = state
	if dc != nil {
		ctrl.debug = dc
	}
	return newAPIHarness(t, ctrl, DebugRoutes)
}

type debugCall struct{ method, path, body string }

var debugCalls = []debugCall{
	{http.MethodPost, routeDebugPause, ``},
	{http.MethodPost, routeDebugResume, `{}`},
	{http.MethodPost, routeDebugStep, `{}`},
	{http.MethodPost, routeDebugBreakpoints, `{"locations":["a.yaml:3"]}`},
	{http.MethodGet, routeDebugVariables, ``},
	{http.MethodPost, routeAct, `{"step":"- tap: Sign in\n"}`},
}

func (h *apiHarness) send(c debugCall) *httptest.ResponseRecorder {
	if c.method == http.MethodGet {
		return h.get(c.path)
	}
	return h.post(c.path, c.body)
}

func TestDebugRoutes_AreAllOperatorActions(t *testing.T) {
	want := map[string]string{}
	for _, c := range debugCalls {
		want[c.path] = c.method
	}
	for _, r := range DebugRoutes(&stubController{}) {
		if want[r.Path] != r.Method || !r.Operator {
			t.Errorf("route %s %s: operator = %v, want a listed operator route", r.Method, r.Path, r.Operator)
		}
		delete(want, r.Path)
	}
	if len(want) != 0 {
		t.Errorf("routes missing from the group: %v", want)
	}
}

func TestDebugRoutes_EveryRouteNeedsTheToken(t *testing.T) {
	h := debugHarness(t, session.StatePaused, &fakeDebugClient{})
	for _, c := range debugCalls {
		rec := h.srv.do(call{method: c.method, path: c.path, body: c.body, header: map[string]string{"Origin": "http://" + h.srv.hostPort()}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", c.path, rec.Code)
		}
	}
}

func TestDebugRoutes_AnOperatorDebugRequestResetsTheIdleTimer(t *testing.T) {
	h := newHostHarness(t)
	dc := &fakeDebugClient{}
	h.first.debug = dc
	h.first.info.State = session.StatePaused
	h.clk.Advance(testIdleMs - 1)
	srv, err := New(Config{Source: h.host, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{h.host.Routes(DebugRoutes)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	dh := &apiHarness{srv: srv, ctrl: h.first.stubController, token: authorize(t, srv)}
	if rec := dh.post(routeDebugResume, `{}`); rec.Code != http.StatusOK {
		t.Fatalf("resume = %d, body %s", rec.Code, rec.Body)
	}
	h.clk.Advance(1)
	h.clk.AwaitSleepers(1)
	if h.released() {
		t.Fatal("the device was released although a debug request ran one millisecond ago")
	}
	if got := dc.called(); len(got) != 1 || got[0] != "resume" {
		t.Fatalf("calls through the host = %v, want one resume", got)
	}
}

func TestDebugRoutes_RefuseEverythingWhenTheHolderOffersNoDebugging(t *testing.T) {
	for _, state := range []session.HolderState{session.StatePaused, session.StateRunning} {
		h := debugHarness(t, state, nil)
		for _, c := range debugCalls {
			rec := h.send(c)
			if rec.Code != http.StatusNotImplemented || codeOf(t, rec) != diag.CodeUnsupported {
				t.Errorf("%s in state %s = %d %s, want 501 UNSUPPORTED", c.path, state, rec.Code, rec.Body)
			}
		}
	}
}

func TestDebugRoutes_OnlyPauseAndBreakpointsAreAdmittedWhileRunning(t *testing.T) {
	dc := &fakeDebugClient{}
	h := debugHarness(t, session.StateRunning, dc)
	admitted := map[string]bool{routeDebugPause: true, routeDebugBreakpoints: true}
	for _, c := range debugCalls {
		rec := h.send(c)
		if admitted[c.path] {
			if rec.Code != http.StatusOK {
				t.Errorf("%s while running = %d, want 200 (body %s)", c.path, rec.Code, rec.Body)
			}
			continue
		}
		if rec.Code != http.StatusConflict || codeOf(t, rec) != diag.CodeHolderBusy {
			t.Errorf("%s while running = %d %s, want 409 HOLDER_BUSY", c.path, rec.Code, rec.Body)
		}
	}
	if got := strings.Join(dc.called(), ","); got != "pause,breakpoints" {
		t.Errorf("client calls = %s, want only pause and breakpoints", got)
	}
}

func TestDebugRoutes_PausedRunForwardsEveryRoute(t *testing.T) {
	dc := &fakeDebugClient{}
	h := debugHarness(t, session.StatePaused, dc)
	for _, c := range debugCalls {
		if rec := h.send(c); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, body %s", c.path, rec.Code, rec.Body)
		}
	}
	if got := strings.Join(dc.called(), ","); got != "pause,resume,resume,breakpoints,variables,act" {
		t.Errorf("client calls = %s", got)
	}
	if dc.actStep != "- tap: Sign in\n" {
		t.Errorf("act step = %q, want the posted YAML", dc.actStep)
	}
	if len(dc.breakpoints) != 1 || dc.breakpoints[0] != "a.yaml:3" {
		t.Errorf("breakpoints = %v, want the posted list", dc.breakpoints)
	}
}
