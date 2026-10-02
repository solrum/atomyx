package inspector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

type fakeController struct{}

func (fakeController) Info() session.Info       { return session.Info{} }
func (fakeController) Latest() session.Snapshot { return session.Snapshot{} }
func (fakeController) Subscribe(context.Context) (<-chan session.Observation, error) {
	return nil, errors.New("not used")
}
func (fakeController) FreshTree(context.Context) (session.Snapshot, error) {
	return session.Snapshot{}, errors.New("not used")
}

type counterReader struct{ next byte }

func (c *counterReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = c.next
		c.next++
	}
	return len(p), nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

type logEntry struct {
	level  string
	msg    string
	fields logging.LogContext
}

type recordingLogger struct {
	mu      *sync.Mutex
	entries *[]logEntry
}

func newRecordingLogger() recordingLogger {
	return recordingLogger{mu: &sync.Mutex{}, entries: &[]logEntry{}}
}

func (l recordingLogger) add(level, msg string, ctx logging.LogContext) {
	l.mu.Lock()
	defer l.mu.Unlock()
	*l.entries = append(*l.entries, logEntry{level, msg, ctx})
}
func (l recordingLogger) Debug(msg string, ctx logging.LogContext) { l.add("debug", msg, ctx) }
func (l recordingLogger) Info(msg string, ctx logging.LogContext)  { l.add("info", msg, ctx) }
func (l recordingLogger) Warn(msg string, ctx logging.LogContext)  { l.add("warn", msg, ctx) }
func (l recordingLogger) Error(msg string, _ error, ctx logging.LogContext) {
	l.add("error", msg, ctx)
}
func (l recordingLogger) Child(logging.LogContext) logging.Logger { return l }

func (l recordingLogger) snapshot() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]logEntry(nil), *l.entries...)
}

func okHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
}

func testRoutes(session.Controller) []Route {
	return []Route{
		{Method: http.MethodGet, Path: "/", Handler: okHandler("page")},
		{Method: http.MethodGet, Path: "/api/v1/tree", Handler: okHandler("tree")},
		{Method: http.MethodGet, Path: "/api/v1/events", Handler: okHandler("events")},
		{Method: http.MethodPost, Path: "/api/v1/act", Handler: okHandler("act")},
	}
}

func newTestServer(t *testing.T, groups ...RouteGroup) (*Server, recordingLogger) {
	t.Helper()
	logger := newRecordingLogger()
	srv, err := New(Config{Controller: fakeController{}, Logger: logger, Entropy: &counterReader{}, Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, logger
}

func (s *Server) port4Test() string {
	_, port, _ := net.SplitHostPort(s.Addr())
	return port
}

func (s *Server) hostPort() string { return "127.0.0.1:" + s.port4Test() }

type call struct {
	method, path, host string
	header             map[string]string
	body               string
}

func (s *Server) do(c call) *httptest.ResponseRecorder {
	if c.host == "" {
		c.host = s.hostPort()
	}
	req := httptest.NewRequestWithContext(context.Background(), c.method, c.path, strings.NewReader(c.body))
	req.Host = c.host
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not an error payload: %v", rec.Body.String(), err)
	}
	return body.Error.Code
}

// authorize runs the bootstrap exchange and returns the token.
func authorize(t *testing.T, s *Server) string {
	t.Helper()
	url, err := s.NewBootstrapURL()
	if err != nil {
		t.Fatal(err)
	}
	return exchangeCode(t, s, url[strings.Index(url, "#")+1:])
}

func exchangeCode(t *testing.T, s *Server, code string) string {
	t.Helper()
	rec := s.do(call{
		method: http.MethodPost, path: authPath, body: `{"code":"` + code + `"}`,
		header: map[string]string{"Origin": "http://" + s.hostPort()},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, body %s", rec.Code, rec.Body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("exchange body %q has no token", rec.Body)
	}
	return out.Token
}

func TestNew_BindsOnlyALoopbackAddressWithAChosenPort(t *testing.T) {
	srv, _ := newTestServer(t)
	host, port, err := net.SplitHostPort(srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("Addr = %s, want 127.0.0.1 with a chosen port", srv.Addr())
	}
}

func TestNew_RejectsAMissingControllerOrLogger(t *testing.T) {
	if _, err := New(Config{Logger: newRecordingLogger()}); err == nil {
		t.Fatal("New without a controller succeeded")
	}
	if _, err := New(Config{Controller: fakeController{}}); err == nil {
		t.Fatal("New without a logger succeeded")
	}
}

func TestHost_AForeignHostIsRejectedOnEveryPath(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	token := authorize(t, srv)
	port := srv.port4Test()
	for _, host := range []string{
		"evil.example:" + port,
		"127.0.0.1:1",
		"127.0.0.1",
		"localhost.evil.example:" + port,
		"[::1]:" + port,
	} {
		for _, path := range []string{"/", "/api/v1/tree"} {
			rec := srv.do(call{method: http.MethodGet, path: path, host: host, header: map[string]string{tokenHeader: token}})
			if rec.Code != http.StatusForbidden || codeOf(t, rec) != codeHostNotAllowed {
				t.Errorf("host %q path %s: status %d %s, want 403 HOST_NOT_ALLOWED", host, path, rec.Code, rec.Body)
			}
		}
	}
}

func TestHost_LoopbackIPAndLocalhostAreAccepted(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	for _, host := range []string{"127.0.0.1:" + srv.port4Test(), "localhost:" + srv.port4Test(), "LocalHost:" + srv.port4Test()} {
		if rec := srv.do(call{method: http.MethodGet, path: "/", host: host}); rec.Code != http.StatusOK {
			t.Errorf("host %q: status %d", host, rec.Code)
		}
	}
}

func TestOrigin_ANonGetRequestNeedsTheServedOrigin(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	token := authorize(t, srv)
	served := "http://" + srv.hostPort()
	for name, origin := range map[string]string{
		"missing":      "",
		"foreign":      "http://evil.example",
		"other port":   "http://127.0.0.1:1",
		"other scheme": "https://" + srv.hostPort(),
		"null":         "null",
	} {
		header := map[string]string{tokenHeader: token}
		if origin != "" {
			header["Origin"] = origin
		}
		rec := srv.do(call{method: http.MethodPost, path: "/api/v1/act", header: header})
		if rec.Code != http.StatusForbidden || codeOf(t, rec) != codeOriginNotAllowed {
			t.Errorf("%s origin: status %d %s, want 403 ORIGIN_NOT_ALLOWED", name, rec.Code, rec.Body)
		}
	}
	rec := srv.do(call{method: http.MethodPost, path: "/api/v1/act", header: map[string]string{tokenHeader: token, "Origin": served}})
	if rec.Code != http.StatusOK {
		t.Fatalf("served origin: status %d %s", rec.Code, rec.Body)
	}
}

func TestOrigin_TheAuthExchangeChecksItToo(t *testing.T) {
	srv, _ := newTestServer(t)
	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]
	rec := srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"` + code + `"}`, header: map[string]string{"Origin": "http://evil.example"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	exchangeCode(t, srv, code)
}

func TestToken_EveryApiRouteNeedsItIncludingGetAndTheEventStream(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	token := authorize(t, srv)
	served := map[string]string{"Origin": "http://" + srv.hostPort()}
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tree"},
		{http.MethodGet, "/api/v1/events"},
		{http.MethodGet, "/api/v1/unknown"},
		{http.MethodPost, "/api/v1/act"},
	}
	for _, r := range routes {
		for name, header := range map[string]map[string]string{
			"missing": served,
			"wrong":   {tokenHeader: "wrong", "Origin": served["Origin"]},
			"empty":   {tokenHeader: "", "Origin": served["Origin"]},
		} {
			rec := srv.do(call{method: r.method, path: r.path, header: header})
			if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != codeTokenRequired {
				t.Errorf("%s %s with %s token: status %d, want 401", r.method, r.path, name, rec.Code)
			}
		}
		rec := srv.do(call{method: r.method, path: r.path + "?token=" + token})
		if r.method == http.MethodGet && rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s with the token in the query: status %d, want 401", r.path, rec.Code)
		}
	}
	for _, path := range []string{"/api/v1/tree", "/api/v1/events"} {
		rec := srv.do(call{method: http.MethodGet, path: path, header: map[string]string{tokenHeader: token}})
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s with the token: status %d", path, rec.Code)
		}
	}
	rec := srv.do(call{method: http.MethodGet, path: "/api/v1/unknown", header: map[string]string{tokenHeader: token}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route with the token: status %d, want 404", rec.Code)
	}
}

func TestToken_OnlyThePageIsExempt(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	rec := srv.do(call{method: http.MethodGet, path: "/"})
	if rec.Code != http.StatusOK || rec.Body.String() != "page" {
		t.Fatalf("GET /: status %d body %q", rec.Code, rec.Body)
	}
}

func TestBootstrap_ACodeIsSingleUse(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	url, err := srv.NewBootstrapURL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:"+srv.port4Test()+"/#") {
		t.Fatalf("url = %q, want the loopback IP, the port and a fragment", url)
	}
	code := url[strings.Index(url, "#")+1:]
	token := exchangeCode(t, srv, code)
	if rec := srv.do(call{method: http.MethodGet, path: "/api/v1/tree", header: map[string]string{tokenHeader: token}}); rec.Code != http.StatusOK {
		t.Fatalf("the issued token was refused: %d", rec.Code)
	}
	rec := srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"` + code + `"}`, header: map[string]string{"Origin": "http://" + srv.hostPort()}})
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != codeCodeRefused {
		t.Fatalf("second use: status %d %s, want 401 CODE_REFUSED", rec.Code, rec.Body)
	}
}

func TestBootstrap_AWrongOrMalformedCodeIsRefusedAndTheRealOneStillWorks(t *testing.T) {
	srv, _ := newTestServer(t)
	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]
	origin := map[string]string{"Origin": "http://" + srv.hostPort()}
	rec := srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"wrong"}`, header: origin})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: status %d", rec.Code)
	}
	rec = srv.do(call{method: http.MethodPost, path: authPath, body: `not json`, header: origin})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: status %d", rec.Code)
	}
	exchangeCode(t, srv, code)
}

func TestBootstrap_NoCodeMintedMeansEveryExchangeIsRefused(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":""}`, header: map[string]string{"Origin": "http://" + srv.hostPort()}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestBootstrap_AFreshCodeReplacesAnUnusedOneAndKeepsIssuedTokens(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	oldToken := authorize(t, srv)
	first, _ := srv.NewBootstrapURL()
	second, _ := srv.NewBootstrapURL()
	firstCode, secondCode := first[strings.Index(first, "#")+1:], second[strings.Index(second, "#")+1:]

	rec := srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"` + firstCode + `"}`, header: map[string]string{"Origin": "http://" + srv.hostPort()}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replaced code: status %d, want 401", rec.Code)
	}
	newToken := exchangeCode(t, srv, secondCode)
	for _, token := range []string{oldToken, newToken} {
		if rec := srv.do(call{method: http.MethodGet, path: "/api/v1/tree", header: map[string]string{tokenHeader: token}}); rec.Code != http.StatusOK {
			t.Fatalf("a token was invalidated by minting: status %d", rec.Code)
		}
	}
}

func TestBootstrap_AnEntropyFailureIsReportedAndIssuesNothing(t *testing.T) {
	srv, err := New(Config{Controller: fakeController{}, Logger: newRecordingLogger(), Entropy: failingReader{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if _, err := srv.NewBootstrapURL(); err == nil {
		t.Fatal("NewBootstrapURL succeeded without entropy")
	}
}

func TestExchange_IsLoggedWithoutTheSecrets(t *testing.T) {
	srv, logger := newTestServer(t)
	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]
	origin := map[string]string{"Origin": "http://" + srv.hostPort()}
	srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"wrong"}`, header: origin})
	token := exchangeCode(t, srv, code)

	entries := logger.snapshot()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want a refusal and an issue", entries)
	}
	if entries[0].msg != "inspector.auth.refused" || entries[0].level != "warn" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].msg != "inspector.auth.issued" || entries[1].level != "info" {
		t.Errorf("second entry = %+v", entries[1])
	}
	for _, e := range entries {
		if e.fields["remote"] == nil || e.fields["remote"] == "" {
			t.Errorf("entry %+v does not name the remote", e)
		}
		for _, v := range e.fields {
			if s, ok := v.(string); ok && (strings.Contains(s, code) || strings.Contains(s, token)) {
				t.Errorf("entry %+v leaks a secret", e)
			}
		}
	}
}

func TestResponses_CarryNoStoreAndTheCSPAndNoCORSHeaders(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	token := authorize(t, srv)
	responses := map[string]*httptest.ResponseRecorder{
		"page":          srv.do(call{method: http.MethodGet, path: "/"}),
		"api ok":        srv.do(call{method: http.MethodGet, path: "/api/v1/tree", header: map[string]string{tokenHeader: token}}),
		"api 401":       srv.do(call{method: http.MethodGet, path: "/api/v1/tree"}),
		"origin 403":    srv.do(call{method: http.MethodPost, path: "/api/v1/act", header: map[string]string{tokenHeader: token}}),
		"preflight":     srv.do(call{method: http.MethodOptions, path: "/api/v1/tree", header: map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "GET"}}),
		"unknown route": srv.do(call{method: http.MethodGet, path: "/nope"}),
	}
	for name, rec := range responses {
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", name, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; img-src 'self' blob:" {
			t.Errorf("%s: Content-Security-Policy = %q", name, got)
		}
		for key := range rec.Header() {
			if strings.HasPrefix(strings.ToLower(key), "access-control-") {
				t.Errorf("%s: carries CORS header %s", name, key)
			}
		}
	}
	if responses["preflight"].Code != http.StatusForbidden {
		t.Errorf("a cross-origin preflight got status %d, want 403", responses["preflight"].Code)
	}
}

func TestRoutes_GroupsReceiveTheControllerAndRegisterWithoutEditingTheServer(t *testing.T) {
	var got session.Controller
	group := func(c session.Controller) []Route {
		got = c
		return []Route{{Method: http.MethodGet, Path: "/api/v1/extra", Handler: okHandler("extra")}}
	}
	ctrl := fakeController{}
	srv, err := New(Config{Controller: ctrl, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: []RouteGroup{group, testRoutes}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	if got != session.Controller(ctrl) {
		t.Fatal("the group did not receive the configured controller")
	}
	token := authorize(t, srv)
	if rec := srv.do(call{method: http.MethodGet, path: "/api/v1/extra", header: map[string]string{tokenHeader: token}}); rec.Body.String() != "extra" {
		t.Fatalf("extra route body %q", rec.Body)
	}
}

func TestRoutes_InvalidAndDuplicateRoutesAreRejected(t *testing.T) {
	bad := map[string][]Route{
		"duplicate":   {{Method: "GET", Path: "/api/v1/a", Handler: okHandler("")}, {Method: "GET", Path: "/api/v1/a", Handler: okHandler("")}},
		"auth path":   {{Method: "POST", Path: authPath, Handler: okHandler("")}},
		"outside api": {{Method: "GET", Path: "/static/x", Handler: okHandler("")}},
		"wildcard":    {{Method: "GET", Path: "/api/{x}", Handler: okHandler("")}},
		"no method":   {{Path: "/api/v1/a", Handler: okHandler("")}},
		"no handler":  {{Method: "GET", Path: "/api/v1/a"}},
	}
	for name, routes := range bad {
		routes := routes
		srv, err := New(Config{Controller: fakeController{}, Logger: newRecordingLogger(), Groups: []RouteGroup{func(session.Controller) []Route { return routes }}})
		if err == nil {
			_ = srv.Close()
			t.Errorf("%s: New succeeded", name)
		}
	}
}

func TestServe_AnswersOverARealConnectionAndStopsOnClose(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()

	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]
	base := "http://" + srv.Addr()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+authPath, strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || out.Token == "" {
		t.Fatalf("exchange over the wire: status %d token %q", resp.StatusCode, out.Token)
	}
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/tree", nil)
	req.Header.Set(tokenHeader, out.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized GET over the wire: status %d", resp.StatusCode)
	}

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v, want nil after Close", err)
	}
}
