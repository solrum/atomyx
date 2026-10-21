package script

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseHTTPStep(t *testing.T, steps string, env map[string]string) *HTTPRequestStep {
	t.Helper()
	def, err := ParseScript(steps, env)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	step, ok := def.Steps[0].(*HTTPRequestStep)
	if !ok {
		t.Fatalf("step 0 is %T, want *HTTPRequestStep", def.Steps[0])
	}
	return step
}

func TestParseScript_HTTPRequestMinimalDefaultsToGet(t *testing.T) {
	step := parseHTTPStep(t, "- httpRequest:\n    url: http://localhost:8025/api/v1/messages\n", nil)
	if step.Method != HTTPMethodGet || step.URL != "http://localhost:8025/api/v1/messages" {
		t.Errorf("step = %+v", step)
	}
	if step.Headers != nil || step.Body != nil || step.As != "" || step.Poll != nil || step.TimeoutMs != nil {
		t.Errorf("optional fields should be unset: %+v", step)
	}
}

func TestParseScript_HTTPRequestAllFields(t *testing.T) {
	step := parseHTTPStep(t, `- httpRequest:
    method: post
    url: https://mail.example.test/search
    headers:
      Authorization: Bearer abc
      X-Count: 3
    body:
      query: hello
      limit: 5
    as: inbox
    expectStatus: 201
    poll:
      until: $.body.items[0]
      interval: 500
    timeout: 30000
`, nil)
	if step.Method != "POST" || step.Headers["Authorization"] != "Bearer abc" || step.Headers["X-Count"] != "3" {
		t.Errorf("step = %+v", step)
	}
	body, ok := step.Body.(OrderedMap)
	if !ok || body.Len() != 2 {
		t.Fatalf("body = %#v, want an object", step.Body)
	}
	if step.As != "inbox" || *step.ExpectStatus != 201 || *step.TimeoutMs != 30000 {
		t.Errorf("step = %+v", step)
	}
	if step.Poll == nil || step.Poll.Until != "$.body.items[0]" || *step.Poll.IntervalMs != 500 {
		t.Errorf("poll = %+v", step.Poll)
	}
	data, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"body":{"query":"hello","limit":5}`) {
		t.Errorf("body should marshal in declaration order: %s", data)
	}
}

func TestParseScript_HTTPRequestStringBody(t *testing.T) {
	step := parseHTTPStep(t, "- httpRequest:\n    method: PUT\n    url: http://localhost/x\n    body: plain text\n", nil)
	if step.Body != "plain text" {
		t.Errorf("body = %#v", step.Body)
	}
}

func TestParseScript_HTTPRequestQueryPlaceholdersAreEscaped(t *testing.T) {
	step := parseHTTPStep(t,
		"- httpRequest:\n    url: \"http://localhost:8025/${path}?query=to:${email}&n=${n}\"\n",
		map[string]string{"path": "a b/c", "email": "a+b@x.test", "n": "1&x=2"})
	want := "http://localhost:8025/a b/c?query=to:a%2Bb%40x.test&n=1%26x%3D2"
	if step.URL != want {
		t.Errorf("url = %q, want %q", step.URL, want)
	}
	authored := AuthoredOf(step).(*HTTPRequestStep)
	if authored.URL != "http://localhost:8025/${path}?query=to:${email}&n=${n}" {
		t.Errorf("authored url = %q", authored.URL)
	}
}

func TestParseScript_HTTPRequestAuthoredFormKeepsSecretPlaceholders(t *testing.T) {
	step := parseHTTPStep(t,
		"- httpRequest:\n    url: http://localhost/x\n    headers:\n      Authorization: \"Bearer ${SECRET}\"\n    body:\n      token: \"${SECRET}\"\n",
		secretEnv)
	if step.Headers["Authorization"] != "Bearer "+secretValue {
		t.Errorf("executed header = %q", step.Headers["Authorization"])
	}
	authored := AuthoredOf(step).(*HTTPRequestStep)
	if authored.Headers["Authorization"] != "Bearer ${SECRET}" {
		t.Errorf("authored header = %q", authored.Headers["Authorization"])
	}
	if WrittenFormLost(step) {
		t.Error("the written form should be rebuilt")
	}
}

func TestParseScript_HTTPRequestRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"bare step", "- httpRequest\n", "httpRequest"},
		{"not an object", "- httpRequest: http://localhost\n", "expected an object"},
		{"missing url", "- httpRequest:\n    method: GET\n", "httpRequest.url"},
		{"unknown field", "- httpRequest:\n    url: http://localhost\n    timeoutMs: 5\n", `unknown field "timeoutMs"`},
		{"bad method", "- httpRequest:\n    method: TRACE\n    url: http://localhost\n", "steps[0].method: must be one of"},
		{"file scheme", "- httpRequest:\n    url: file:///etc/passwd\n", "steps[0].url: must be an absolute http or https URL"},
		{"relative url", "- httpRequest:\n    url: /api/x\n", "steps[0].url: must be an absolute http or https URL"},
		{"header name", "- httpRequest:\n    url: http://localhost\n    headers:\n      \"Bad Name\": x\n", "not a valid header name"},
		{"header newline", "- httpRequest:\n    url: http://localhost\n    headers:\n      X-A: \"a\\nb\"\n", "must not contain a line break"},
		{"bad body", "- httpRequest:\n    url: http://localhost\n    body: [1, 2]\n", "httpRequest.body"},
		{"bad as", "- httpRequest:\n    url: http://localhost\n    as: \"1bad\"\n", "steps[0].as: variable names"},
		{"status range", "- httpRequest:\n    url: http://localhost\n    expectStatus: 700\n", "expectStatus: must be between 100 and 599"},
		{"timeout too long", "- httpRequest:\n    url: http://localhost\n    timeout: 120001\n", "timeout: must be between 1 and 120000"},
		{"timeout zero", "- httpRequest:\n    url: http://localhost\n    timeout: 0\n", "timeout: must be between"},
		{"poll without as", "- httpRequest:\n    url: http://localhost\n    poll:\n      until: $.body\n", "poll: requires `as`"},
		{"poll path", "- httpRequest:\n    url: http://localhost\n    as: r\n    poll:\n      until: body\n", "poll.until: must be a JSON path"},
		{"poll interval", "- httpRequest:\n    url: http://localhost\n    as: r\n    poll:\n      until: $.body\n      interval: 199\n", "poll.interval: must be at least 200"},
		{"poll unknown", "- httpRequest:\n    url: http://localhost\n    as: r\n    poll:\n      until: $.b\n      every: 5\n", `poll: unknown field "every"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseScript_HTTPRequestErrorsNeverEchoTheURL(t *testing.T) {
	_, err := ParseScript("- httpRequest:\n    url: \"ftp://user:hunter2@host/x\"\n", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

func TestParseScript_ExtractObjectForm(t *testing.T) {
	def, err := ParseScript(`- extract:
    from: inbox
    values:
      plain: $.body.a
      otp:
        path: $.body.messages[0].Snippet
        regex: "\\b(\\d{6})\\b"
      whole:
        path: $.body.b
`, nil)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	step := def.Steps[0].(*ExtractStep)
	if got := strings.Join(step.Values.Keys(), ","); got != "plain,otp,whole" {
		t.Errorf("keys = %s", got)
	}
	plain, _ := step.Values.Get("plain")
	otp, _ := step.Values.Get("otp")
	whole, _ := step.Values.Get("whole")
	if plain.Path != "$.body.a" || plain.Regex != nil {
		t.Errorf("plain = %+v", plain)
	}
	if otp.Path != "$.body.messages[0].Snippet" || otp.Regex == nil || *otp.Regex != `\b(\d{6})\b` {
		t.Errorf("otp = %+v", otp)
	}
	if whole.Regex != nil {
		t.Errorf("whole = %+v", whole)
	}
	data, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	want := `"values":{"plain":"$.body.a","otp":{"path":"$.body.messages[0].Snippet","regex":"\\b(\\d{6})\\b"},"whole":"$.body.b"}`
	if !strings.Contains(string(data), want) {
		t.Errorf("json = %s\nwant it to contain %s", data, want)
	}
}

func TestParseScript_ExtractObjectFormRejections(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing path", "- extract:\n    from: r\n    values:\n      v:\n        regex: x\n", "extract.values.v.path"},
		{"unknown key", "- extract:\n    from: r\n    values:\n      v:\n        path: $.a\n        group: 1\n", `unknown field "group"`},
		{"regex type", "- extract:\n    from: r\n    values:\n      v:\n        path: $.a\n        regex: 5\n", "extract.values.v.regex: expected a string"},
		{"bad regex", "- extract:\n    from: r\n    values:\n      v:\n        path: $.a\n        regex: \"(\"\n", "values.v.regex: not a valid regular expression"},
		{"number value", "- extract:\n    from: r\n    values:\n      v: 5\n", "Expected string, received number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseScript(tc.yaml, nil)
			if err == nil {
				t.Fatalf("ParseScript accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
