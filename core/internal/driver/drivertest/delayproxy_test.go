package drivertest

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testHold  = 400 * time.Millisecond
	testBound = 100 * time.Millisecond
)

func TestHTTPDelayProxy_HoldsOnlyTheFirstMatchingRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	port := NewHTTPDelayProxy(t, backend.Listener.Addr().String(), "/slow", testHold)
	client := &http.Client{Timeout: testBound}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	if resp, err := get(t, client, base+"/slow"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("first /slow request answered within the bound, want it held")
	}
	for _, path := range []string{"/slow", "/fast"} {
		resp, err := get(t, client, base+path)
		if err != nil {
			t.Fatalf("GET %s after the held request: %v", path, err)
		}
		_ = resp.Body.Close()
	}
}

func TestLineDelayProxy_HoldsOnlyTheMatchingReplyAndKeepsTheConnectionUsable(t *testing.T) {
	backend, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = backend.Close() }()
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var id int64
			if _, err := fmt.Sscanf(scanner.Text(), `{"id":%d`, &id); err == nil {
				_, _ = fmt.Fprintf(conn, "{\"id\":%d,\"ok\":true}\n", id)
			}
		}
	}()
	port, delivered := NewLineDelayProxy(t, backend.Addr().String(), "slow", testHold)
	conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)

	_, _ = io.WriteString(conn, "{\"id\":1,\"type\":\"slow\",\"args\":{}}\n")
	_, _ = io.WriteString(conn, "{\"id\":2,\"type\":\"fast\",\"args\":{}}\n")
	_ = conn.SetReadDeadline(time.Now().Add(testBound * 3))
	if line, err := reader.ReadString('\n'); err != nil || line != "{\"id\":2,\"ok\":true}\n" {
		t.Fatalf("first reply = %q, %v, want the unheld reply for id 2", line, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(testHold * 3))
	if line, err := reader.ReadString('\n'); err != nil || line != "{\"id\":1,\"ok\":true}\n" {
		t.Fatalf("held reply = %q, %v, want the delayed reply for id 1", line, err)
	}
	select {
	case <-delivered:
	case <-time.After(testBound):
		t.Fatal("delivered channel still open after the held reply arrived")
	}
}

func TestLineDelayProxy_CleanupDoesNotWaitForAClientThatNeverDisconnects(t *testing.T) {
	backend, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = backend.Close() }()
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t.Run("proxy", func(t *testing.T) {
			port, _ := NewLineDelayProxy(t, backend.Addr().String(), "slow", testHold)
			conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Fatalf("dial proxy: %v", err)
			}
			_ = conn
		})
	}()
	select {
	case <-done:
	case <-time.After(testHold * 5):
		t.Fatal("proxy cleanup hung on an open client connection")
	}
}

func get(t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return client.Do(req)
}
