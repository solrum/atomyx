package httpclient

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/runner"
)

func hostOf(t *testing.T, server *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func kindOf(t *testing.T, err error) runner.HTTPFailure {
	t.Helper()
	var httpErr *runner.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error %v is not a classified HTTP error", err)
	}
	return httpErr.Kind
}

func get(url string) runner.HTTPRequest {
	return runner.HTTPRequest{Method: "GET", URL: url}
}

func TestClient_ReturnsStatusHeadersAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "abc" || r.Method != "POST" {
			t.Errorf("method %s header %q", r.Method, r.Header.Get("X-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Options{AllowHosts: []string{hostOf(t, server)}})
	resp, err := client.Do(context.Background(), runner.HTTPRequest{
		Method: "POST", URL: server.URL + "/x", Headers: map[string]string{"X-Token": "abc"}, Body: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Status != 201 || string(resp.Body) != `{"ok":true}` || resp.Headers["Content-Type"] != "application/json" {
		t.Errorf("response = %+v", resp)
	}
}

func TestClient_EmptyAllowlistRefusesEverything(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the server must not be reached")
	}))
	defer server.Close()
	for _, hosts := range [][]string{nil, {}} {
		_, err := New(Options{AllowHosts: hosts}).Do(context.Background(), get(server.URL))
		if kindOf(t, err) != runner.HTTPFailureNoHostsAllowed {
			t.Errorf("kind = %v", err)
		}
		if !strings.Contains(err.Error(), "http.allowHosts") {
			t.Errorf("message %q must name the field", err.Error())
		}
	}
}

func TestClient_UnlistedHostIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the server must not be reached")
	}))
	defer server.Close()
	_, err := New(Options{AllowHosts: []string{"other.example.test"}}).Do(context.Background(), get(server.URL))
	if kindOf(t, err) != runner.HTTPFailureHostNotAllowed {
		t.Errorf("err = %v", err)
	}
}

func TestAllowlist_Patterns(t *testing.T) {
	cases := []struct {
		entries []string
		target  string
		want    bool
	}{
		{[]string{"localhost"}, "http://localhost:9999/x", true},
		{[]string{"localhost:8025"}, "http://localhost:8025/x", true},
		{[]string{"localhost:8025"}, "http://localhost:8026/x", false},
		{[]string{"mail.example.test"}, "https://mail.example.test/x", true},
		{[]string{"MAIL.example.test"}, "https://mail.EXAMPLE.test/x", true},
		{[]string{"*.example.test"}, "https://mail.example.test/x", true},
		{[]string{"*.example.test"}, "https://a.b.example.test/x", true},
		{[]string{"*.example.test"}, "https://example.test/x", false},
		{[]string{"*.example.test"}, "https://evilexample.test/x", false},
		{[]string{"*.example.test:8443"}, "https://mail.example.test:8443/x", true},
		{[]string{"*.example.test:8443"}, "https://mail.example.test/x", false},
		{[]string{"example.test:443"}, "https://example.test/x", true},
		{[]string{"example.test:80"}, "http://example.test/x", true},
		{[]string{"[::1]:8025"}, "http://[::1]:8025/x", true},
		{[]string{"[::1]"}, "http://[::1]:9/x", true},
	}
	for _, tc := range cases {
		target, err := url.Parse(tc.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := newAllowlist(tc.entries).allows(target); got != tc.want {
			t.Errorf("%v allows %s = %v, want %v", tc.entries, tc.target, got, tc.want)
		}
	}
}

func TestClient_RedirectToDisallowedHostIsRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the redirect target must not be reached")
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+portOf(t, other)+"/x", http.StatusFound)
	}))
	defer origin.Close()

	client := New(Options{AllowHosts: []string{"127.0.0.1"}})
	_, err := client.Do(context.Background(), get(origin.URL))
	if kindOf(t, err) != runner.HTTPFailureRedirectNotAllowed {
		t.Errorf("err = %v", err)
	}
}

func portOf(t *testing.T, server *httptest.Server) string {
	t.Helper()
	u, _ := url.Parse(server.URL)
	return u.Port()
}

func TestClient_FollowsRedirectsWithinTheLimit(t *testing.T) {
	hops := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hops < runner.HTTPMaxRedirects {
			hops++
			http.Redirect(w, r, server.URL+"/next", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()
	resp, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(context.Background(), get(server.URL))
	if err != nil || string(resp.Body) != "done" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}

func TestClient_TooManyRedirects(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/again", http.StatusFound)
	}))
	defer server.Close()
	_, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(context.Background(), get(server.URL))
	if kindOf(t, err) != runner.HTTPFailureTooManyRedirects {
		t.Errorf("err = %v", err)
	}
}

func TestClient_LinkLocalIsRefusedEvenWhenListed(t *testing.T) {
	client := New(Options{AllowHosts: []string{"169.254.169.254", "[fe80::1]", "169.254.0.0"}})
	for _, target := range []string{"http://169.254.169.254/latest/meta-data", "http://[fe80::1]:8080/", "http://169.254.0.0/"} {
		_, err := client.Do(context.Background(), get(target))
		if kindOf(t, err) != runner.HTTPFailureAddressRefused {
			t.Errorf("%s: err = %v", target, err)
		}
	}
}

func TestClient_ResponseBodyCap(t *testing.T) {
	cases := []struct {
		name  string
		size  int
		allow bool
	}{
		{"exactly the cap", runner.HTTPMaxResponseBodyBytes, true},
		{"one byte over", runner.HTTPMaxResponseBodyBytes + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(strings.Repeat("a", tc.size)))
			}))
			defer server.Close()
			resp, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(context.Background(), get(server.URL))
			if tc.allow {
				if err != nil || len(resp.Body) != tc.size {
					t.Fatalf("len %d err %v", len(resp.Body), err)
				}
				return
			}
			if kindOf(t, err) != runner.HTTPFailureBodyTooLarge {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestClient_ChunkedBodyOverTheCapIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher := w.(http.Flusher)
		chunk := []byte(strings.Repeat("a", 64*1024))
		for i := 0; i < 20; i++ {
			_, _ = w.Write(chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()
	_, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(context.Background(), get(server.URL))
	if kindOf(t, err) != runner.HTTPFailureBodyTooLarge {
		t.Errorf("err = %v", err)
	}
}

func TestClient_IgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer server.Close()
	resp, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(context.Background(), get(server.URL))
	if err != nil || string(resp.Body) != "direct" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}

func TestClient_ConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := hostOf(t, server)
	server.Close()
	_, err := New(Options{AllowHosts: []string{address}}).Do(context.Background(), get("http://"+address+"/secret-path?token=abc"))
	if kindOf(t, err) != runner.HTTPFailureConnectionRefused {
		t.Fatalf("err = %v", err)
	}
	for _, leaked := range []string{"secret-path", "token=abc", address} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("error text %q leaks %q", err.Error(), leaked)
		}
	}
}

func TestClient_TimeoutIsClassified(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(ctx, get(server.URL))
	if kindOf(t, err) != runner.HTTPFailureTimeout {
		t.Errorf("err = %v", err)
	}
}

func TestClient_CanceledContextReturnsContextError(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := New(Options{AllowHosts: []string{hostOf(t, server)}}).Do(ctx, get(server.URL))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestClient_TLSVerifiesUnlessTheCAIsTrusted(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secure"))
	}))
	defer server.Close()
	allow := []string{hostOf(t, server)}

	_, err := New(Options{AllowHosts: allow}).Do(context.Background(), get(server.URL))
	if kindOf(t, err) != runner.HTTPFailureTLS {
		t.Fatalf("an untrusted certificate must fail verification, err = %v", err)
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}
	if err := os.WriteFile(caFile, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, err := New(Options{AllowHosts: allow, CAFile: caFile}).Do(context.Background(), get(server.URL))
	if err != nil || string(resp.Body) != "secure" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}

func TestClient_UnreadableCAFileFailsRequests(t *testing.T) {
	client := New(Options{AllowHosts: []string{"localhost"}, CAFile: filepath.Join(t.TempDir(), "missing.pem")})
	_, err := client.Do(context.Background(), get("http://localhost:1/"))
	if kindOf(t, err) != runner.HTTPFailureCAFile {
		t.Errorf("err = %v", err)
	}
}

func TestClient_InvalidCAFileContentFailsRequests(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{AllowHosts: []string{"localhost"}, CAFile: caFile}).Do(context.Background(), get("http://localhost:1/"))
	if kindOf(t, err) != runner.HTTPFailureCAFile {
		t.Errorf("err = %v", err)
	}
}

func TestClient_RejectsNonHTTPSchemes(t *testing.T) {
	_, err := New(Options{AllowHosts: []string{"localhost"}}).Do(context.Background(), get("file:///etc/passwd"))
	if kindOf(t, err) != runner.HTTPFailureOther {
		t.Errorf("err = %v", err)
	}
}
