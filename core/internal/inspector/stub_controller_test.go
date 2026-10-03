package inspector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/session"
)

// stubController is a Source whose answers a test sets.
type stubController struct {
	mu         sync.Mutex
	info       session.Info
	snap       session.Snapshot
	feed       chan session.Observation
	subscribe  error
	freshSnap  session.Snapshot
	freshErr   error
	freshCalls int
	latestRead int
	// holder is the lease state the host last recorded, empty until it
	// records one. duringFresh runs inside FreshTree.
	holder      string
	duringFresh func()
}

func (c *stubController) Info() session.Info { return c.info }

func (c *stubController) Latest() session.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestRead++
	return c.snap
}

func (c *stubController) Subscribe(context.Context) (<-chan session.Observation, error) {
	if c.subscribe != nil {
		return nil, c.subscribe
	}
	return c.feed, nil
}

func (c *stubController) SetIdle() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.holder = "idle"
	return nil
}

func (c *stubController) SetRunning(string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.holder = "running"
	return nil
}

func (c *stubController) holderState() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder
}

func (c *stubController) FreshTree(context.Context) (session.Snapshot, error) {
	c.mu.Lock()
	during := c.duringFresh
	c.mu.Unlock()
	if during != nil {
		during()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.freshCalls++
	return c.freshSnap, c.freshErr
}

// apiHarness is a server over a stub controller with an issued token.
type apiHarness struct {
	srv   *Server
	ctrl  *stubController
	token string
}

func newAPIHarness(t *testing.T, ctrl *stubController, groups ...RouteGroup) *apiHarness {
	t.Helper()
	srv, err := New(Config{Source: ctrl, Logger: newRecordingLogger(), Entropy: &counterReader{}, Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return &apiHarness{srv: srv, ctrl: ctrl, token: authorize(t, srv)}
}

func (h *apiHarness) get(path string) *httptest.ResponseRecorder {
	return h.srv.do(call{method: http.MethodGet, path: path, header: map[string]string{tokenHeader: h.token}})
}

func (h *apiHarness) post(path, body string) *httptest.ResponseRecorder {
	return h.srv.do(call{
		method: http.MethodPost, path: path, body: body,
		header: map[string]string{tokenHeader: h.token, "Origin": "http://" + h.srv.hostPort()},
	})
}

func containsAll(t *testing.T, body string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Fatalf("body %q does not contain %q", body, p)
		}
	}
}
