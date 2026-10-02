package inspector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

const (
	secretText = "S3cretPass"
	treeRev    = 3
)

func node(attrs map[string]string, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: attrs, Children: children}
}

// loginTree is a screen with an email field, a secure password field
// that still carries a secret in its text and value (the inspector must
// not rely on the secret having been masked), a button, and a dialog
// drawn over the button.
func loginTree() wire.TreeNodeWire {
	return node(map[string]string{"role": "container"},
		node(map[string]string{"role": "text-field", "id": "email", "label": "Email", "bounds": "0,200,100,260"}),
		node(map[string]string{
			"role": "text-field", "id": "pw", "label": "Password", "secure": "true",
			"text": secretText, "value": secretText, "bounds": "0,300,100,360",
		}),
		node(map[string]string{"role": "button", "label": "Sign in", "text": "Sign in", "bounds": "0,0,100,100"}),
		node(map[string]string{"role": "dialog", "id": "promo", "bounds": "0,0,200,200"}),
	)
}

func storedLogin() *stubController {
	return &stubController{snap: session.Snapshot{Tree: loginTree(), HasTree: true, TreeEpoch: 2, TreeRevision: treeRev}}
}

func selectorHarness(t *testing.T, ctrl *stubController) *apiHarness {
	return newAPIHarness(t, ctrl, SelectorRoutes)
}

func TestSelectorRoutes_AreAllOperatorActions(t *testing.T) {
	want := map[string]bool{routeSelectorSuggest: true, routeSelectorTest: true}
	for _, r := range SelectorRoutes(&stubController{}) {
		if !want[r.Path] || !r.Operator || r.Method != http.MethodPost {
			t.Errorf("route %s %s: operator = %v, want a listed POST operator action", r.Method, r.Path, r.Operator)
		}
		delete(want, r.Path)
	}
	if len(want) != 0 {
		t.Errorf("routes missing from the group: %v", want)
	}
}

func TestSelectorRoutes_RefuseAFetchWithoutTheTokenOrOrigin(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	for _, route := range SelectorRoutes(h.ctrl) {
		noToken := h.srv.do(call{method: http.MethodPost, path: route.Path, header: map[string]string{"Origin": "http://" + h.srv.hostPort()}})
		noOrigin := h.srv.do(call{method: http.MethodPost, path: route.Path, header: map[string]string{tokenHeader: h.token}})
		if noToken.Code != http.StatusUnauthorized || noOrigin.Code != http.StatusForbidden {
			t.Errorf("%s: without token = %d, without origin = %d, want 401 and 403", route.Path, noToken.Code, noOrigin.Code)
		}
	}
}

func suggestBody(rev uint64, path string) string {
	return fmt.Sprintf(`{"treeRevision":%d,"path":%s}`, rev, path)
}

type suggestOut struct {
	TreeRevision uint64 `json:"treeRevision"`
	Candidates   []struct {
		Selector   map[string]any `json:"selector"`
		Unique     bool           `json:"unique"`
		MatchCount int            `json:"matchCount"`
		Obscured   bool           `json:"obscured"`
	} `json:"candidates"`
}

func decodeSuggest(t *testing.T, body string) suggestOut {
	t.Helper()
	var out suggestOut
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return out
}

func TestSuggest_ARevisionThatIsNotTheStoredOneIsRefused(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	for _, rev := range []uint64{0, treeRev - 1, treeRev + 1} {
		rec := h.post(routeSelectorSuggest, suggestBody(rev, "[0]"))
		if rec.Code != http.StatusConflict || codeOf(t, rec) != codeTreeStale {
			t.Errorf("revision %d: status = %d code = %q, want 409 %s", rev, rec.Code, codeOf(t, rec), codeTreeStale)
		}
	}
}

func TestSuggest_AReplacedTreeNeverYieldsASuggestionForTheWrongNode(t *testing.T) {
	ctrl := storedLogin()
	h := selectorHarness(t, ctrl)
	if rec := h.post(routeSelectorSuggest, suggestBody(treeRev, "[0]")); rec.Code != http.StatusOK {
		t.Fatalf("before the tree changes: status = %d", rec.Code)
	}
	ctrl.mu.Lock()
	ctrl.snap.TreeRevision = treeRev + 1
	ctrl.snap.Tree = node(map[string]string{"role": "container"}, node(map[string]string{"role": "button", "id": "other"}))
	ctrl.mu.Unlock()
	rec := h.post(routeSelectorSuggest, suggestBody(treeRev, "[0]"))
	if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "other") {
		t.Fatalf("after the tree changes: status = %d body %s, want a 409 with no suggestion", rec.Code, rec.Body)
	}
}

func TestSuggest_SuggestsFromTheStoredTreeAndCarriesRevision(t *testing.T) {
	rec := selectorHarness(t, storedLogin()).post(routeSelectorSuggest, suggestBody(treeRev, "[0]"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	out := decodeSuggest(t, rec.Body.String())
	if out.TreeRevision != treeRev || len(out.Candidates) == 0 {
		t.Fatalf("response = %+v", out)
	}
	first := out.Candidates[0]
	if first.Selector["id"] != "email" || !first.Unique {
		t.Errorf("first candidate = %+v, want the unique id selector", first)
	}
}

func TestSuggest_ASecureNodeNeverUsesItsTextOrValue(t *testing.T) {
	rec := selectorHarness(t, storedLogin()).post(routeSelectorSuggest, suggestBody(treeRev, "[1]"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secretText) {
		t.Fatalf("the response contains the secret: %s", rec.Body)
	}
	out := decodeSuggest(t, rec.Body.String())
	if len(out.Candidates) == 0 || out.Candidates[0].Selector["id"] != "pw" {
		t.Fatalf("candidates = %+v, want the id selector first", out.Candidates)
	}
}

func TestSuggest_CarriesWhetherTheTargetIsObscured(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	covered := decodeSuggest(t, h.post(routeSelectorSuggest, suggestBody(treeRev, "[2]")).Body.String())
	free := decodeSuggest(t, h.post(routeSelectorSuggest, suggestBody(treeRev, "[0]")).Body.String())
	if len(covered.Candidates) == 0 || len(free.Candidates) == 0 {
		t.Fatal("no candidates")
	}
	for _, c := range covered.Candidates {
		if !c.Obscured {
			t.Errorf("candidate for the covered button = %+v, want obscured", c)
		}
	}
	for _, c := range free.Candidates {
		if c.Obscured {
			t.Errorf("candidate for the free field = %+v, want not obscured", c)
		}
	}
}

func TestSuggest_RejectsABadRequestWithACodedError(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	cases := map[string]string{
		"no path":      `{"treeRevision":3}`,
		"path outside": suggestBody(treeRev, "[9]"),
		"negative":     suggestBody(treeRev, "[-1]"),
		"not json":     `nope`,
		"unknown key":  `{"treeRevision":3,"path":[],"extra":1}`,
		"trailing":     suggestBody(treeRev, "[]") + `{}`,
		"empty body":   ``,
		"wrong type":   `{"treeRevision":"3","path":[]}`,
	}
	for name, body := range cases {
		rec := h.post(routeSelectorSuggest, body)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != diag.CodeInvalidArgument {
			t.Errorf("%s: status = %d code = %q, want 400 %s", name, rec.Code, codeOf(t, rec), diag.CodeInvalidArgument)
		}
	}
}

func TestSuggest_AnEmptyPathSuggestsForTheRoot(t *testing.T) {
	rec := selectorHarness(t, storedLogin()).post(routeSelectorSuggest, suggestBody(treeRev, "[]"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestSuggest_NoTreeIsACodedNotFound(t *testing.T) {
	rec := selectorHarness(t, &stubController{}).post(routeSelectorSuggest, suggestBody(1, "[]"))
	if rec.Code != http.StatusNotFound || codeOf(t, rec) != codeNoTree {
		t.Fatalf("status = %d code = %q, want 404 %s", rec.Code, codeOf(t, rec), codeNoTree)
	}
}

type testOut struct {
	TreeRevision uint64 `json:"treeRevision"`
	Count        int    `json:"count"`
	Truncated    bool   `json:"truncated"`
	Matches      []struct {
		Path   []int `json:"path"`
		Bounds *struct{ Left, Top, Right, Bottom float64 }
	} `json:"matches"`
}

func runSelectorTest(t *testing.T, h *apiHarness, selector string) testOut {
	t.Helper()
	rec := h.post(routeSelectorTest, `{"selector":`+selector+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var out testOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSelectorTest_ReturnsMatchesWithPathsAndBounds(t *testing.T) {
	out := runSelectorTest(t, selectorHarness(t, storedLogin()), `{"id":"email"}`)
	if out.Count != 1 || len(out.Matches) != 1 || out.TreeRevision != treeRev {
		t.Fatalf("out = %+v", out)
	}
	m := out.Matches[0]
	if len(m.Path) != 1 || m.Path[0] != 0 || m.Bounds == nil || m.Bounds.Top != 200 || m.Bounds.Bottom != 260 {
		t.Fatalf("match = %+v", m)
	}
}

func TestSelectorTest_UsesPriorityBroadeningLikeTheRuntime(t *testing.T) {
	out := runSelectorTest(t, selectorHarness(t, storedLogin()), `{"text":"Password"}`)
	if out.Count != 1 || out.Matches[0].Path[0] != 1 {
		t.Fatalf("a text selector should reach the node whose label is Password: %+v", out)
	}
}

func TestSelectorTest_AcceptsRegexRoleAndStateSelectors(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	if out := runSelectorTest(t, h, `{"label":{"regex":"^Pass"}}`); out.Count != 1 {
		t.Errorf("regex: count = %d, want 1", out.Count)
	}
	if out := runSelectorTest(t, h, `{"role":"text-field"}`); out.Count != 2 {
		t.Errorf("role: count = %d, want 2", out.Count)
	}
	if out := runSelectorTest(t, h, `{"role":"text-field","nth":1}`); out.Count != 1 || out.Matches[0].Path[0] != 1 {
		t.Errorf("role and nth: %+v", out)
	}
}

func TestSelectorTest_NoMatchIsAnEmptyList(t *testing.T) {
	rec := selectorHarness(t, storedLogin()).post(routeSelectorTest, `{"selector":{"id":"missing"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"matches":[]`) || !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestSelectorTest_AnEmptySelectorIsInvalidArgument(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	for _, body := range []string{`{"selector":{}}`, `{}`, `{"selector":{"nth":0}}`} {
		rec := h.post(routeSelectorTest, body)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != diag.CodeInvalidArgument {
			t.Errorf("%s: status = %d code = %q, want 400 %s", body, rec.Code, codeOf(t, rec), diag.CodeInvalidArgument)
		}
	}
}

func TestSelectorTest_RejectsABadRegexAndNoTree(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	if rec := h.post(routeSelectorTest, `{"selector":{"id":{"regex":"(?=x)"}}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unsupported regex: status = %d, want 400", rec.Code)
	}
	rec := selectorHarness(t, &stubController{}).post(routeSelectorTest, `{"selector":{"id":"a"}}`)
	if rec.Code != http.StatusNotFound || codeOf(t, rec) != codeNoTree {
		t.Errorf("no tree: status = %d code = %q", rec.Code, codeOf(t, rec))
	}
}

func TestSelectorTest_CapsTheListedMatches(t *testing.T) {
	children := make([]wire.TreeNodeWire, maxReportedMatches+5)
	for i := range children {
		children[i] = node(map[string]string{"role": "button"})
	}
	ctrl := &stubController{snap: session.Snapshot{Tree: node(map[string]string{"role": "container"}, children...), HasTree: true, TreeRevision: 1}}
	out := runSelectorTest(t, selectorHarness(t, ctrl), `{"role":"button"}`)
	if out.Count != maxReportedMatches+5 || len(out.Matches) != maxReportedMatches || !out.Truncated {
		t.Fatalf("count = %d, listed = %d, truncated = %v", out.Count, len(out.Matches), out.Truncated)
	}
}
