package inspector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/script"
)

type yamlOut struct {
	YAML        string `json:"yaml"`
	Secure      bool   `json:"secure"`
	Placeholder string `json:"placeholder"`
}

func renderYAML(t *testing.T, h *apiHarness, body string) (yamlOut, int, *httptest.ResponseRecorder) {
	t.Helper()
	rec := h.post(routeStepYAML, body)
	var out yamlOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out, rec.Code, rec
}

func TestStepYAML_RendersEverySupportedActionAndItParsesBack(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	cases := map[string]struct{ body, command string }{
		"tap":              {`{"action":"tap","selector":{"id":"email"}}`, script.CommandTap},
		"assertVisible":    {`{"action":"assertVisible","selector":{"text":"Sign in"}}`, script.CommandAssertVisible},
		"assertNotVisible": {`{"action":"assertNotVisible","selector":{"id":"promo"}}`, script.CommandAssertNotVisible},
		"assertExists":     {`{"action":"assertExists","selector":{"label":"Email"}}`, script.CommandAssertExists},
		"waitFor":          {`{"action":"waitFor","selector":{"id":"promo"}}`, script.CommandWaitFor},
		"type":             {`{"action":"type","text":"me@example.com","selector":{"id":"email"}}`, script.CommandType},
	}
	for name, tc := range cases {
		out, code, rec := renderYAML(t, h, tc.body)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", name, code, rec.Body)
		}
		def, err := script.ParseScript(out.YAML, nil)
		if err != nil {
			t.Fatalf("%s: yaml does not parse:\n%s\n%v", name, out.YAML, err)
		}
		if len(def.Steps) != 1 || def.Steps[0].StepCommand() != tc.command {
			t.Errorf("%s: parsed steps = %+v, want one %s", name, def.Steps, tc.command)
		}
		if out.Secure || out.Placeholder != "" {
			t.Errorf("%s: secure = %v placeholder = %q, want neither", name, out.Secure, out.Placeholder)
		}
	}
}

func TestStepYAML_RendersTheDocumentedShapes(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	cases := map[string]string{
		`{"action":"tap","selector":{"text":"Sign in"}}`:            "- tap: Sign in\n",
		`{"action":"tap","selector":{"role":"text-field","nth":1}}`: "- tap:\n    role: text-field\n    nth: 1\n",
		`{"action":"type","text":"me","selector":{"id":"email"}}`:   "- type:\n    into:\n      id: email\n    text: me\n",
		`{"action":"assertVisible","selector":{"id":"promo"}}`:      "- assertVisible:\n    id: promo\n",
	}
	for body, want := range cases {
		if out, _, rec := renderYAML(t, h, body); out.YAML != want {
			t.Errorf("%s: yaml = %q, want %q (body %s)", body, out.YAML, want, rec.Body)
		}
	}
}

func TestStepYAML_ASecureTargetRendersAnEnvPlaceholderNeverTheText(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	out, code, rec := renderYAML(t, h, `{"action":"type","text":"hunter2typed","selector":{"id":"pw"}}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %s", code, rec.Body)
	}
	if !out.Secure || out.Placeholder != "password" || !strings.Contains(out.YAML, "text: ${password}") {
		t.Fatalf("out = %+v, want the placeholder", out)
	}
	if strings.Contains(rec.Body.String(), "hunter2typed") || strings.Contains(rec.Body.String(), secretText) {
		t.Fatalf("the response contains a secret: %s", rec.Body)
	}
}

func TestStepYAML_ASecureTargetFoundThroughBroadeningStillUsesThePlaceholder(t *testing.T) {
	out, _, _ := renderYAML(t, selectorHarness(t, storedLogin()), `{"action":"type","text":"x","selector":{"text":"Password"}}`)
	if !out.Secure || !strings.Contains(out.YAML, "text: ${password}") {
		t.Fatalf("out = %+v", out)
	}
}

func TestStepYAML_TypeIntoAnUnknownTargetFailsClosed(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	_, code, rec := renderYAML(t, h, `{"action":"type","text":"secret","selector":{"id":"nowhere"}}`)
	if code != http.StatusNotFound || codeOf(t, rec) != codeNodeNotFound {
		t.Fatalf("status = %d code = %q, want 404 %s", code, codeOf(t, rec), codeNodeNotFound)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("the response echoes the text: %s", rec.Body)
	}
	_, code, rec = renderYAML(t, selectorHarness(t, &stubController{}), `{"action":"type","text":"x","selector":{"id":"a"}}`)
	if code != http.StatusNotFound || codeOf(t, rec) != codeNoTree {
		t.Fatalf("no tree: status = %d code = %q, want 404 %s", code, codeOf(t, rec), codeNoTree)
	}
}

func TestStepYAML_OtherActionsNeedNoTree(t *testing.T) {
	_, code, rec := renderYAML(t, selectorHarness(t, &stubController{}), `{"action":"tap","selector":{"id":"a"}}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %s", code, rec.Body)
	}
}

func TestStepYAML_RejectsWhatItCannotRenderWithACodedError(t *testing.T) {
	h := selectorHarness(t, storedLogin())
	cases := map[string]string{
		"unknown action": `{"action":"swipe","selector":{"id":"a"}}`,
		"no action":      `{"selector":{"id":"a"}}`,
		"empty selector": `{"action":"tap","selector":{}}`,
		"nth only":       `{"action":"tap","selector":{"nth":2}}`,
		"text on tap":    `{"action":"tap","text":"x","selector":{"id":"a"}}`,
		"regex":          `{"action":"tap","selector":{"id":{"regex":"a.*"}}}`,
		"value":          `{"action":"tap","selector":{"value":"a"}}`,
		"enabled":        `{"action":"tap","selector":{"id":"a","enabled":true}}`,
		"clickable":      `{"action":"tap","selector":{"id":"a","clickable":true}}`,
		"focused":        `{"action":"tap","selector":{"id":"a","focused":true}}`,
		"negative nth":   `{"action":"tap","selector":{"id":"a","nth":-1}}`,
		"unknown field":  `{"action":"tap","selector":{"id":"a"},"x":1}`,
		"not json":       `[`,
	}
	for name, body := range cases {
		rec := h.post(routeStepYAML, body)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != diag.CodeInvalidArgument {
			t.Errorf("%s: status = %d code = %q, want 400 %s (body %s)", name, rec.Code, codeOf(t, rec), diag.CodeInvalidArgument, rec.Body)
		}
	}
}

func TestStepYAML_ListsTheSupportedActionsInTheRemedy(t *testing.T) {
	rec := selectorHarness(t, storedLogin()).post(routeStepYAML, `{"action":"swipe","selector":{"id":"a"}}`)
	containsAll(t, rec.Body.String(), "tap", "assertVisible", "assertNotVisible", "assertExists", "waitFor", "type")
}
