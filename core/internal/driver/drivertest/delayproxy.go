package drivertest

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// NewHTTPDelayProxy starts a loopback HTTP proxy in front of the agent
// listening at target (host:port) and returns the proxy's port. The
// first request whose path equals holdPath is held for hold before it
// is forwarded, so a client with a shorter bound gives up on it while
// every other request, including later ones for the same path, passes
// straight through. The proxy stops when the test ends.
func NewHTTPDelayProxy(t *testing.T, target, holdPath string, hold time.Duration) int {
	t.Helper()
	upstream := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: target})
	var held atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == holdPath && held.CompareAndSwap(false, true) {
			select {
			case <-time.After(hold):
			case <-r.Context().Done():
				return
			}
		}
		upstream.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return listenerPort(t, server.Listener.Addr())
}

// NewLineDelayProxy starts a loopback TCP proxy in front of the agent
// listening at target (host:port) and returns the proxy's port. It
// speaks the iOS runner's line-delimited JSON protocol: the reply to
// the first request whose "type" equals holdType is delivered after
// hold, while every other reply is forwarded immediately. Delivery of
// the held reply does not block replies to later requests on the same
// connection. The proxy stops when the test ends.
func NewLineDelayProxy(t *testing.T, target, holdType string, hold time.Duration) int {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("delay proxy listen: %v", err)
	}
	var (
		held   atomic.Bool
		heldID atomic.Int64
		wg     sync.WaitGroup
	)
	heldID.Store(-1)
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveLineProxy(t, client, target, holdType, hold, &held, &heldID)
			}()
		}
	}()
	return listenerPort(t, listener.Addr())
}

type lineEnvelope struct {
	ID   *int64 `json:"id"`
	Type string `json:"type"`
}

func serveLineProxy(t *testing.T, client net.Conn, target, holdType string, hold time.Duration, held *atomic.Bool, heldID *atomic.Int64) {
	t.Helper()
	upstream, err := new(net.Dialer).DialContext(t.Context(), "tcp", target)
	if err != nil {
		_ = client.Close()
		return
	}
	var clientWrite sync.Mutex
	writeClient := func(line []byte) {
		clientWrite.Lock()
		defer clientWrite.Unlock()
		_, _ = client.Write(line)
	}
	var timers sync.WaitGroup
	closeBoth := func() {
		_ = client.Close()
		_ = upstream.Close()
	}
	defer func() {
		closeBoth()
		timers.Wait()
	}()

	go func() {
		defer closeBoth()
		reader := bufio.NewReader(client)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				var env lineEnvelope
				if json.Unmarshal(line, &env) == nil && env.ID != nil && env.Type == holdType && held.CompareAndSwap(false, true) {
					heldID.Store(*env.ID)
				}
				if _, werr := upstream.Write(line); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	reader := bufio.NewReader(upstream)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var env lineEnvelope
			if json.Unmarshal(line, &env) == nil && env.ID != nil && *env.ID == heldID.Load() {
				heldID.Store(-1)
				timers.Add(1)
				go func(line []byte) {
					defer timers.Done()
					time.Sleep(hold)
					writeClient(line)
				}(line)
			} else {
				writeClient(line)
			}
		}
		if err != nil {
			if err != io.EOF && !errors.Is(err, net.ErrClosed) {
				t.Logf("delay proxy upstream read: %v", err)
			}
			return
		}
	}
}

func listenerPort(t *testing.T, addr net.Addr) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("delay proxy address %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("delay proxy port %q: %v", portText, err)
	}
	return port
}
