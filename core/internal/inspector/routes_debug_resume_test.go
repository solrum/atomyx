package inspector

import (
	"encoding/json"
	"net/http"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

func TestDebugRoutes_ResumeAndStepSelectTheModeAndPassForce(t *testing.T) {
	cases := []struct {
		path      string
		body      string
		step, frc bool
	}{
		{routeDebugResume, `{}`, false, false},
		{routeDebugResume, `{"force":true}`, false, true},
		{routeDebugStep, `{}`, true, false},
		{routeDebugStep, `{"force":true}`, true, true},
	}
	for _, tc := range cases {
		dc := &fakeDebugClient{}
		h := debugHarness(t, session.StatePaused, dc)
		if rec := h.post(tc.path, tc.body); rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d, body %s", tc.path, tc.body, rec.Code, rec.Body)
		}
		if dc.resumeOpts.Step != tc.step || dc.resumeOpts.Force != tc.frc {
			t.Errorf("%s %s: options = %+v, want step %v force %v", tc.path, tc.body, dc.resumeOpts, tc.step, tc.frc)
		}
	}
}

func TestDebugRoutes_AMissedResumeReturnsTheReasonWithStatusOK(t *testing.T) {
	dc := &fakeDebugClient{resumeOut: session.ResumeResult{Resumed: false, MissReason: "no node matches id login"}}
	h := debugHarness(t, session.StatePaused, dc)
	rec := h.post(routeDebugResume, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var out session.ResumeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Resumed || out.MissReason != "no node matches id login" {
		t.Errorf("out = %+v, want the miss reason and resumed false", out)
	}
}

func TestDebugRoutes_VariablesAndActReturnWhatTheHolderReports(t *testing.T) {
	h := debugHarness(t, session.StatePaused, &fakeDebugClient{})
	rec := h.get(routeDebugVariables)
	containsAll(t, rec.Body.String(), `"user":"ann"`, `"password":"****"`)
	rec = h.post(routeAct, `{"step":"- tap: x\n"}`)
	containsAll(t, rec.Body.String(), `"command":"tap"`, `"outcome":"passed"`)
}

func TestDebugRoutes_HolderErrorsKeepTheirStatusMapping(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
	}{
		diag.CodeHolderBusy:      {diag.NewHolderBusy("an act is running"), http.StatusConflict},
		diag.CodeUnsupported:     {diag.NewUnsupported("debug.pause"), http.StatusNotImplemented},
		diag.CodeInvalidArgument: {diag.New(diag.CodeInvalidArgument, "bad location", "fix it", nil), http.StatusBadRequest},
	}
	for code, tc := range cases {
		h := debugHarness(t, session.StatePaused, &fakeDebugClient{err: tc.err})
		for _, c := range debugCalls {
			rec := h.send(c)
			if rec.Code != tc.status || codeOf(t, rec) != code {
				t.Errorf("%s with %s = %d %s, want %d", c.path, code, rec.Code, rec.Body, tc.status)
			}
		}
	}
}

func TestDebugRoutes_BodiesAreChecked(t *testing.T) {
	h := debugHarness(t, session.StatePaused, &fakeDebugClient{})
	bad := []debugCall{
		{http.MethodPost, routeDebugResume, `{"forced":true}`},
		{http.MethodPost, routeDebugBreakpoints, `{}`},
		{http.MethodPost, routeAct, `{"step":""}`},
		{http.MethodPost, routeAct, `{}`},
	}
	for _, c := range bad {
		if rec := h.send(c); rec.Code != http.StatusBadRequest || codeOf(t, rec) != diag.CodeInvalidArgument {
			t.Errorf("%s %s = %d %s, want 400 INVALID_ARGUMENT", c.path, c.body, rec.Code, rec.Body)
		}
	}
}

func TestDebugRoutes_AnEmptyBreakpointListClearsThem(t *testing.T) {
	dc := &fakeDebugClient{breakpoints: []string{"old"}}
	h := debugHarness(t, session.StateRunning, dc)
	if rec := h.post(routeDebugBreakpoints, `{"locations":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if dc.breakpoints == nil || len(dc.breakpoints) != 0 {
		t.Errorf("breakpoints = %#v, want an empty non-nil list", dc.breakpoints)
	}
}
