package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/script"
)

// serverDoer sends requests with the standard client, enforcing the
// response size cap the way a real adapter does, so these tests exercise
// the command against real HTTP servers.
type serverDoer struct{}

func (serverDoer) Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return HTTPResponse{}, err
	}
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return HTTPResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, HTTPMaxResponseBodyBytes+1))
	if err != nil {
		return HTTPResponse{}, err
	}
	if len(body) > HTTPMaxResponseBodyBytes {
		return HTTPResponse{}, &HTTPError{Kind: HTTPFailureBodyTooLarge}
	}
	headers := map[string]string{}
	for name, values := range resp.Header {
		headers[name] = strings.Join(values, ", ")
	}
	return HTTPResponse{Status: resp.StatusCode, Headers: headers, Body: body}, nil
}

type doerFunc func(ctx context.Context, req HTTPRequest) (HTTPResponse, error)

func (f doerFunc) Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error) { return f(ctx, req) }

func newHTTPTestRunner(doer HTTPDoer) *testRunner {
	tr := newTestRunner()
	tr.runner = NewScriptRunner(ScriptRunnerDeps{
		Orchestra:      tr.orch,
		Clock:          tr.clock,
		Logger:         noopLogger{},
		NetworkCapture: tr.capture,
		FlowLoader:     tr.loader,
		Commands:       DefaultCommands(),
		HTTP:           doer,
	})
	return tr
}

func httpScript(steps string) string { return "appId: com.test\n---\n" + steps }

func TestHTTPRequest_SuccessStoresTheResponseLikeCapture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Key") != "k" {
			t.Errorf("method %s content-type %q key %q", r.Method, r.Header.Get("Content-Type"), r.Header.Get("X-Key"))
		}
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["name"] != "ada" {
			t.Errorf("body = %v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":7,"messages":[{"Snippet":"Your code is 482913."}]}`))
	}))
	defer server.Close()

	tr := newHTTPTestRunner(serverDoer{})
	result := run(t, tr, httpScript(fmt.Sprintf(`- httpRequest:
    method: POST
    url: %s/api
    headers:
      X-Key: k
    body:
      name: ada
    as: created
    expectStatus: 201
- assertApi:
    from: created
    status: 201
    body:
      $.id: 7
- extract:
    from: created
    values:
      id: $.body.id
- assertExpr:
    expr: "id == \"7\""
`, server.URL)))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	want := fmt.Sprintf(`POST %s/api returned status 201 as "created"`, server.URL)
	if got := detail(result.Steps[0]); got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}

func TestHTTPRequest_StringBodyIsSentAsWritten(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != "a=1&b=2" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("body %q type %q", data, r.Header.Get("Content-Type"))
		}
	}))
	defer server.Close()
	result := run(t, newHTTPTestRunner(serverDoer{}), httpScript(fmt.Sprintf(`- httpRequest:
    method: PUT
    url: %s
    headers:
      Content-Type: application/x-www-form-urlencoded
    body: "a=1&b=2"
`, server.URL)))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestHTTPRequest_StatusRules(t *testing.T) {
	cases := []struct {
		name   string
		status int
		extra  string
		ok     bool
		want   string
	}{
		{"2xx passes by default", 204, "", true, "returned status 204"},
		{"5xx fails by default", 503, "", false, "returned status 503, expected a 2xx status"},
		{"3xx fails by default", 304, "", false, "returned status 304, expected a 2xx status"},
		{"expected 404 passes", 404, "    expectStatus: 404\n", true, "returned status 404"},
		{"expected 201 rejects 200", 200, "    expectStatus: 201\n", false, "returned status 200, expected 201"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer server.Close()
			result := run(t, newHTTPTestRunner(serverDoer{}), httpScript("- httpRequest:\n    url: "+server.URL+"\n"+tc.extra))
			if result.Ok != tc.ok {
				t.Fatalf("Ok = %v, steps = %s", result.Ok, debugSteps(result.Steps))
			}
			if got := detail(lastStep(t, result)); !strings.Contains(got, tc.want) {
				t.Errorf("detail %q does not contain %q", got, tc.want)
			}
		})
	}
}

func TestHTTPRequest_PollRepeatsUntilThePathHasAValue(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			_, _ = w.Write([]byte(`{"messages":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"messages":[{"Snippet":"code 123456"}]}`))
	}))
	defer server.Close()

	tr := newHTTPTestRunner(serverDoer{})
	result := run(t, tr, httpScript(fmt.Sprintf(`- httpRequest:
    url: %s
    as: inbox
    poll:
      until: $.body.messages[0].Snippet
      interval: 500
    timeout: 30000
- extract:
    from: inbox
    values:
      otp:
        path: $.body.messages[0].Snippet
        regex: "\\b(\\d{6})\\b"
- assertExpr:
    expr: "otp == \"123456\""
`, server.URL)))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if got := detail(result.Steps[0]); !strings.HasSuffix(got, "after 3 attempts") {
		t.Errorf("detail = %q", got)
	}
	if now := tr.clock.NowMs(); now != 1000 {
		t.Errorf("clock = %d, want two 500ms sleeps on the runner clock", now)
	}
}

func TestHTTPRequest_PollTimeoutNamesThePathAndLastStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer server.Close()
	tr := newHTTPTestRunner(serverDoer{})
	result := run(t, tr, httpScript(fmt.Sprintf(`- httpRequest:
    url: %s
    as: inbox
    poll:
      until: $.body.messages[0]
      interval: 1000
    timeout: 3500
`, server.URL)))
	if result.Ok {
		t.Fatal("the poll should time out")
	}
	got := detail(lastStep(t, result))
	want := `did not satisfy poll.until "$.body.messages[0]" within 3500ms after 4 attempts (last status 200)`
	if !strings.Contains(got, want) {
		t.Errorf("detail = %q, want it to contain %q", got, want)
	}
	if now := tr.clock.NowMs(); now > 3500 {
		t.Errorf("clock = %d, the poll must not run past its timeout", now)
	}
}

func TestHTTPRequest_PollValueRules(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"missing", `{}`, false},
		{"null", `{"v":null}`, false},
		{"empty string", `{"v":""}`, false},
		{"empty array", `{"v":[]}`, false},
		{"zero counts", `{"v":0}`, true},
		{"false counts", `{"v":false}`, true},
		{"text", `{"v":"x"}`, true},
		{"array item", `{"v":[1]}`, true},
		{"object", `{"v":{}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var decoded any
			if err := json.Unmarshal([]byte(tc.body), &decoded); err != nil {
				t.Fatal(err)
			}
			record := CapturedRequest{Status: 200, Body: decoded}
			if got := pollSatisfied(record, "$.body.v"); got != tc.want {
				t.Errorf("pollSatisfied = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHTTPRequest_PollFailsStatusUntilItPasses(t *testing.T) {
	calls := 0
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		calls++
		if calls < 2 {
			return HTTPResponse{Status: 404, Body: []byte(`{"v":1}`)}, nil
		}
		return HTTPResponse{Status: 200, Body: []byte(`{"v":1}`)}, nil
	})
	result := run(t, newHTTPTestRunner(doer), httpScript(`- httpRequest:
    url: http://localhost/x
    as: r
    poll:
      until: $.body.v
`))
	if !result.Ok || calls != 2 {
		t.Fatalf("ok %v calls %d steps %s", result.Ok, calls, debugSteps(result.Steps))
	}
}

func TestHTTPRequest_SingleRequestTimeout(t *testing.T) {
	doer := doerFunc(func(ctx context.Context, _ HTTPRequest) (HTTPResponse, error) {
		<-ctx.Done()
		return HTTPResponse{}, ctx.Err()
	})
	result := run(t, newHTTPTestRunner(doer), httpScript("- httpRequest:\n    url: http://localhost/x\n    timeout: 20\n"))
	if result.Ok {
		t.Fatal("the request should time out")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "the request timed out after 20ms") {
		t.Errorf("detail = %q", got)
	}
}

func TestHTTPRequest_ContextCancelStopsTheStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		calls++
		cancel()
		return HTTPResponse{Status: 200, Body: []byte(`{}`)}, nil
	})
	tr := newHTTPTestRunner(doer)
	def := mustParse(t, httpScript("- httpRequest:\n    url: http://localhost/x\n    as: r\n    poll:\n      until: $.body.v\n"))
	result, err := tr.runner.Run(ctx, def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatal("a canceled step must fail")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "Interrupted: context canceled") {
		t.Errorf("detail = %q", got)
	}
	if calls != 1 {
		t.Errorf("calls = %d, the poll must stop at the sleep", calls)
	}
}

func TestHTTPRequest_CancelDuringARequestIsReportedAsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		cancel()
		return HTTPResponse{}, context.Canceled
	})
	tr := newHTTPTestRunner(doer)
	result, err := tr.runner.Run(ctx, mustParse(t, httpScript("- httpRequest:\n    url: http://localhost/x\n")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "Interrupted: context canceled") {
		t.Errorf("detail = %q", got)
	}
}

func TestHTTPRequest_ResponseOverTheCapFailsTheStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), HTTPMaxResponseBodyBytes+1))
	}))
	defer server.Close()
	result := run(t, newHTTPTestRunner(serverDoer{}), httpScript("- httpRequest:\n    url: "+server.URL+"\n"))
	if result.Ok {
		t.Fatal("an oversized body must fail the step")
	}
	if got := detail(lastStep(t, result)); !strings.Contains(got, "the response body is larger than 1 MiB") {
		t.Errorf("detail = %q", got)
	}
}

func TestHTTPRequest_BodyAtTheCapIsAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), HTTPMaxResponseBodyBytes))
	}))
	defer server.Close()
	result := run(t, newHTTPTestRunner(serverDoer{}), httpScript("- httpRequest:\n    url: "+server.URL+"\n"))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestHTTPRequest_FailureCategoriesNeverCarryTheExecutedRequest(t *testing.T) {
	const secret = "tok-8f3a9c1d"
	rawErrors := []error{
		errors.New(`Get "http://localhost/path/` + secret + `?q=1": dial tcp 127.0.0.1:1: connect: connection refused`),
		fmt.Errorf("wrapped: %w", errors.New("Post http://localhost/path/"+secret+": EOF")),
	}
	for _, raw := range rawErrors {
		doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) { return HTTPResponse{}, raw })
		result := runWithEnv(t, newHTTPTestRunner(doer),
			httpScript("- httpRequest:\n    url: \"http://localhost/path/${TOKEN}?q=1\"\n    headers:\n      Authorization: \"Bearer ${TOKEN}\"\n"),
			map[string]string{"TOKEN": secret})
		got := detail(lastStep(t, result))
		if result.Ok || strings.Contains(got, secret) || strings.Contains(got, "dial tcp") || strings.Contains(got, "EOF") {
			t.Errorf("detail leaks the transport error: %q", got)
		}
		if !strings.Contains(got, "GET http://localhost/path/${TOKEN}?q=1 failed: the request failed") {
			t.Errorf("detail = %q", got)
		}
	}
}

func TestHTTPRequest_EveryFailureKindHasACategoryText(t *testing.T) {
	kinds := map[HTTPFailure]string{
		HTTPFailureNoHostsAllowed:     "http.allowHosts",
		HTTPFailureHostNotAllowed:     "http.allowHosts",
		HTTPFailureRedirectNotAllowed: "redirect",
		HTTPFailureTooManyRedirects:   "too many redirects",
		HTTPFailureAddressRefused:     "link-local",
		HTTPFailureConnectionRefused:  "connection refused",
		HTTPFailureDNS:                "could not be resolved",
		HTTPFailureTLS:                "TLS verification failed",
		HTTPFailureTimeout:            "timed out",
		HTTPFailureBodyTooLarge:       "1 MiB",
		HTTPFailureCAFile:             "http.caFile",
	}
	for kind, want := range kinds {
		doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
			return HTTPResponse{}, fmt.Errorf("transport: %w", &HTTPError{Kind: kind})
		})
		result := run(t, newHTTPTestRunner(doer), httpScript("- httpRequest:\n    url: http://localhost/x\n"))
		if got := detail(lastStep(t, result)); result.Ok || !strings.Contains(got, want) {
			t.Errorf("%s: detail = %q, want it to contain %q", kind, got, want)
		}
	}
}

func TestHTTPRequest_SecretEnvValueNeverReachesTheDetail(t *testing.T) {
	const secret = "s3cret-mailbox-token"
	var sent HTTPRequest
	doer := doerFunc(func(_ context.Context, req HTTPRequest) (HTTPResponse, error) {
		sent = req
		return HTTPResponse{Status: 500}, nil
	})
	result := runWithEnv(t, newHTTPTestRunner(doer), httpScript(`- httpRequest:
    url: "http://localhost/inbox/${TOKEN}"
    headers:
      Authorization: "Bearer ${TOKEN}"
    body:
      token: "${TOKEN}"
`), map[string]string{"TOKEN": secret})

	if sent.Headers["Authorization"] != "Bearer "+secret || !strings.Contains(sent.URL, secret) || !bytes.Contains(sent.Body, []byte(secret)) {
		t.Fatalf("the executed request should carry the value: %+v", sent)
	}
	got := detail(lastStep(t, result))
	if strings.Contains(got, secret) {
		t.Errorf("detail leaks the secret: %q", got)
	}
	if !strings.Contains(got, "http://localhost/inbox/${TOKEN} returned status 500") {
		t.Errorf("detail = %q", got)
	}
	masked := result.Masked(func(s string) string { return strings.ReplaceAll(s, secret, "***") })
	if strings.Contains(debugSteps(masked.Steps), secret) {
		t.Error("masked steps still carry the secret")
	}
}

func TestHTTPRequest_StepSummaryUsesTheAuthoredForm(t *testing.T) {
	def, err := script.ParseScript(httpScript("- httpRequest:\n    url: \"http://localhost/${TOKEN}\"\n"), map[string]string{"TOKEN": "value-1234"})
	if err != nil {
		t.Fatal(err)
	}
	summary := SummarizeStep(def.Steps[0]).Text
	if summary != "httpRequest: GET http://localhost/${TOKEN}" {
		t.Errorf("summary = %q", summary)
	}
}

func TestHTTPRequest_NoClientConfiguredFailsClearly(t *testing.T) {
	result := run(t, newTestRunner(), httpScript("- httpRequest:\n    url: http://localhost/x\n"))
	if result.Ok {
		t.Fatal("a step with no client must fail")
	}
	if got := detail(lastStep(t, result)); got != "HTTP requests are not available in this session." {
		t.Errorf("detail = %q", got)
	}
}

func TestHTTPRequest_NonJSONBodyStoresNil(t *testing.T) {
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		return HTTPResponse{Status: 200, Headers: map[string]string{"Content-Type": "text/plain"}, Body: []byte("123456")}, nil
	})
	tr := newHTTPTestRunner(doer)
	result := run(t, tr, httpScript(`- httpRequest:
    url: http://localhost/x
    as: r
- assertApi:
    from: r
    body:
      $.x: $exists
`))
	if result.Ok {
		t.Fatalf("$.x cannot exist in a non-JSON body, steps = %s", debugSteps(result.Steps))
	}
}

func TestHTTPRequest_RunsInsideNestedStepLists(t *testing.T) {
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		return HTTPResponse{Status: 200, Body: []byte(`{"v":"x"}`)}, nil
	})
	result := run(t, newHTTPTestRunner(doer), httpScript(`- repeat:
    times: 2
    commands:
      - httpRequest:
          url: http://localhost/x
`))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
}

func TestHTTPRequest_OTPRecipeTypesTheExtractedCodeThroughASubFlow(t *testing.T) {
	doer := doerFunc(func(context.Context, HTTPRequest) (HTTPResponse, error) {
		return HTTPResponse{Status: 200, Body: []byte(`{"messages":[{"Snippet":"Your code is 482913."}]}`)}, nil
	})
	tr := newHTTPTestRunner(doer)
	tr.driver.StageHierarchy(rootTree(focusedButton("field", "", "0,200,100,250")))
	flowPath, _ := filepath.Abs("flows/enter-otp.yml")
	tr.loader.stage(flowPath, "- type: \"${otp}\"\n")
	result := run(t, tr, httpScript(`- httpRequest:
    url: http://localhost:8025/api/v1/messages
    as: inbox
- extract:
    from: inbox
    values:
      otp:
        path: $.body.messages[0].Snippet
        regex: "\\b(\\d{6})\\b"
- runFlow: flows/enter-otp.yml
`))
	if !result.Ok {
		t.Fatalf("steps = %s", debugSteps(result.Steps))
	}
	if texts := typedTexts(tr); len(texts) != 1 || texts[0] != "482913" {
		t.Errorf("typed %v, want the extracted code", texts)
	}
}
