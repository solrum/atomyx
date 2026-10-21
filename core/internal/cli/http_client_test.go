package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/runner"
)

func TestNewScriptHTTPClient_RefusesEverythingWithoutAnAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the server must not be reached")
	}))
	defer server.Close()

	configs := map[string]*config.LoadedConfig{
		"no config":   nil,
		"no http key": {Config: config.AtomyxConfig{}},
	}
	for name, loaded := range configs {
		_, err := newScriptHTTPClient(loaded).Do(context.Background(), runner.HTTPRequest{Method: "GET", URL: server.URL})
		var httpErr *runner.HTTPError
		if !errors.As(err, &httpErr) || httpErr.Kind != runner.HTTPFailureNoHostsAllowed {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNewScriptHTTPClient_AllowsTheListedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	hosts := []string{u.Host}
	loaded := &config.LoadedConfig{Config: config.AtomyxConfig{HTTP: &config.HTTP{AllowHosts: &hosts}}}

	resp, err := newScriptHTTPClient(loaded).Do(context.Background(), runner.HTTPRequest{Method: "GET", URL: server.URL})
	if err != nil || string(resp.Body) != "ok" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}
