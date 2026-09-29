package iossimdriver_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/iossimdriver"
)

// fakeDialer hands back a pre-established net.Conn instead of dialing
// a real socket, so HidClient's WebSocket handshake and frame codec
// run end to end against an in-memory fake helper.
type fakeDialer struct {
	conn net.Conn
}

func (f *fakeDialer) Dial(context.Context, int) (net.Conn, error) { return f.conn, nil }

var _ iossimdriver.Dialer = (*fakeDialer)(nil)

// fakeHidServer plays the atomyx-sim-hid helper's role over one end
// of a net.Pipe: it performs the WS opening handshake, decodes each
// masked client frame, and replies with an unmasked text frame per
// RFC 6455 §5.1. Its frame codec is written independently from
// HidClient's, so a passing test demonstrates real wire compatibility
// rather than the client agreeing with itself.
type fakeHidServer struct {
	conn net.Conn

	mu       sync.Mutex
	received []map[string]any
	respond  func(msg map[string]any) map[string]any
}

func newFakeHidServer(conn net.Conn) *fakeHidServer {
	return &fakeHidServer{conn: conn}
}

func (s *fakeHidServer) setRespond(f func(msg map[string]any) map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = f
}

func (s *fakeHidServer) Received() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.received...)
}

func (s *fakeHidServer) serveHandshake() (*bufio.Reader, error) {
	r := bufio.NewReader(s.conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	if _, err := s.conn.Write([]byte(resp)); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *fakeHidServer) run() {
	r, err := s.serveHandshake()
	if err != nil {
		return
	}
	for {
		msg, err := s.readFrame(r)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.received = append(s.received, msg)
		respond := s.respond
		s.mu.Unlock()

		reply := map[string]any{"ok": true, "id": msg["id"]}
		if respond != nil {
			reply = respond(msg)
		}
		if reply == nil {
			continue
		}
		if err := s.writeFrame(reply); err != nil {
			return
		}
	}
}

func (s *fakeHidServer) readFrame(r *bufio.Reader) (map[string]any, error) {
	head, err := readN(r, 2)
	if err != nil {
		return nil, err
	}
	masked := head[1]&0x80 != 0
	length := int(head[1] & 0x7f)
	switch length {
	case 126:
		ext, err := readN(r, 2)
		if err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext, err := readN(r, 8)
		if err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint64(ext))
	}
	var mask []byte
	if masked {
		mask, err = readN(r, 4)
		if err != nil {
			return nil, err
		}
	}
	payload, err := readN(r, length)
	if err != nil {
		return nil, err
	}
	if mask != nil {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	var msg map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(payload), &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (s *fakeHidServer) writeFrame(msg map[string]any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n := len(line)
	var header []byte
	if n <= 125 {
		header = []byte{0x81, byte(n)}
	} else {
		header = []byte{0x81, 126, byte(n >> 8), byte(n)}
	}
	_, err = s.conn.Write(append(header, line...))
	return err
}

func readN(r *bufio.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

// newConnectedHidClient wires a HidClient to a fakeHidServer over a
// net.Pipe and connects it, failing the test on any error.
func newConnectedHidClient(t *testing.T, opts iossimdriver.HidClientOptions) (*iossimdriver.HidClient, *fakeHidServer) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()

	client := iossimdriver.NewHidClient(&fakeDialer{conn: clientConn}, opts)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Dispose() })
	return client, srv
}

func TestHidClient_Tap_SendsExactPayloadShape(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	if err := client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.25, Y: 0.75}, 50); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	received := srv.Received()
	if len(received) != 1 {
		t.Fatalf("received = %+v, want exactly one message", received)
	}
	msg := received[0]
	want := map[string]any{"type": "tap", "x": 0.25, "y": 0.75, "holdMs": float64(50), "id": float64(1)}
	for k, v := range want {
		if msg[k] != v {
			t.Fatalf("tap message[%s] = %v (%T), want %v (full message %+v)", k, msg[k], msg[k], v, msg)
		}
	}
}

func TestHidClient_LongPress_UsesGivenHoldMs(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	if err := client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.1, Y: 0.2}, 900); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	if got := srv.Received()[0]["holdMs"]; got != float64(900) {
		t.Fatalf("holdMs = %v, want 900", got)
	}
}

func TestHidClient_Swipe_SendsExactPayloadShapeAndStepCount(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	// 160ms / 16ms per step = 10 steps.
	if err := client.Swipe(context.Background(), iossimdriver.NormalizedPoint{X: 0.1, Y: 0.5}, iossimdriver.NormalizedPoint{X: 0.9, Y: 0.5}, 160); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	msg := srv.Received()[0]
	want := map[string]any{
		"type": "swipe", "x1": 0.1, "y1": 0.5, "x2": 0.9, "y2": 0.5,
		"steps": float64(10), "stepMs": float64(16), "dwellMs": float64(0),
	}
	for k, v := range want {
		if msg[k] != v {
			t.Fatalf("swipe message[%s] = %v, want %v (full message %+v)", k, msg[k], v, msg)
		}
	}
}

func TestHidClient_Swipe_CapsStepsAtSixty(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	// 5000ms / 16ms per step would be 313 steps uncapped; must clamp to 60.
	if err := client.Swipe(context.Background(), iossimdriver.NormalizedPoint{}, iossimdriver.NormalizedPoint{X: 1, Y: 1}, 5000); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	if got := srv.Received()[0]["steps"]; got != float64(60) {
		t.Fatalf("steps = %v, want 60 (capped)", got)
	}
}

func TestHidClient_Swipe_FloorsStepsAtOne(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	// 1ms / 16ms per step rounds to 0 steps uncapped; must floor to 1.
	if err := client.Swipe(context.Background(), iossimdriver.NormalizedPoint{}, iossimdriver.NormalizedPoint{X: 1, Y: 1}, 1); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	if got := srv.Received()[0]["steps"]; got != float64(1) {
		t.Fatalf("steps = %v, want 1 (floored)", got)
	}
}

func TestHidClient_TouchDownMoveUp_DispatchWithCorrectTypeField(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	ctx := context.Background()
	if err := client.TouchDown(ctx, iossimdriver.NormalizedPoint{X: 0.2, Y: 0.3}); err != nil {
		t.Fatalf("TouchDown() error = %v", err)
	}
	if err := client.TouchMove(ctx, iossimdriver.NormalizedPoint{X: 0.25, Y: 0.35}); err != nil {
		t.Fatalf("TouchMove() error = %v", err)
	}
	if err := client.TouchUp(ctx, iossimdriver.NormalizedPoint{X: 0.25, Y: 0.35}); err != nil {
		t.Fatalf("TouchUp() error = %v", err)
	}
	received := srv.Received()
	if len(received) != 3 {
		t.Fatalf("received = %+v, want 3 messages", received)
	}
	wantTypes := []string{"touch-down", "touch-move", "touch-up"}
	for i, want := range wantTypes {
		if received[i]["type"] != want {
			t.Fatalf("message[%d].type = %v, want %q", i, received[i]["type"], want)
		}
	}
}

func TestHidClient_IDSequencing_IncrementsFromOnePerDispatch(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	ctx := context.Background()
	if err := client.Tap(ctx, iossimdriver.NormalizedPoint{}, 50); err != nil {
		t.Fatalf("Tap() #1 error = %v", err)
	}
	if err := client.Tap(ctx, iossimdriver.NormalizedPoint{}, 50); err != nil {
		t.Fatalf("Tap() #2 error = %v", err)
	}
	if err := client.Tap(ctx, iossimdriver.NormalizedPoint{}, 50); err != nil {
		t.Fatalf("Tap() #3 error = %v", err)
	}
	received := srv.Received()
	wantIDs := []float64{1, 2, 3}
	for i, want := range wantIDs {
		if received[i]["id"] != want {
			t.Fatalf("message[%d].id = %v, want %v", i, received[i]["id"], want)
		}
	}
}

func TestHidClient_Dispatch_OverwritesCallerSuppliedIDWithCorrelationID(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	// A caller-visible touch contact id is never part of the wire
	// payload HidClient builds; only the dispatch's own correlation
	// id travels as "id". This is exercised indirectly through
	// TouchDown, which never sets an "id" key of its own. Asserting
	// here that the sole "id" present is the sequence id, not
	// anything a caller could have supplied.
	if err := client.TouchDown(context.Background(), iossimdriver.NormalizedPoint{X: 0.5, Y: 0.5}); err != nil {
		t.Fatalf("TouchDown() error = %v", err)
	}
	if got := srv.Received()[0]["id"]; got != float64(1) {
		t.Fatalf("id = %v, want the dispatch correlation id 1", got)
	}
}

func TestHidClient_RejectsOnOkFalseReply(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	srv.setRespond(func(msg map[string]any) map[string]any {
		return map[string]any{"ok": false, "id": msg["id"], "error": "dispatch failed"}
	})
	err := client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.5, Y: 0.5}, 50)
	var hidErr *iossimdriver.HidClientError
	if !errors.As(err, &hidErr) {
		t.Fatalf("Tap() error = %v (%T), want a *HidClientError", err, err)
	}
	if hidErr.Code != iossimdriver.HidClientErrReplyError {
		t.Fatalf("HidClientError.Code = %q, want %q", hidErr.Code, iossimdriver.HidClientErrReplyError)
	}
	if hidErr.Message != "dispatch failed" {
		t.Fatalf("HidClientError.Message = %q, want the server's error text", hidErr.Message)
	}
}

func TestHidClient_TimesOutWhenNoReplyArrives(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1, DispatchTimeout: 30 * time.Millisecond})
	srv.setRespond(func(map[string]any) map[string]any { return nil })
	err := client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.5, Y: 0.5}, 50)
	var hidErr *iossimdriver.HidClientError
	if !errors.As(err, &hidErr) {
		t.Fatalf("Tap() error = %v (%T), want a *HidClientError", err, err)
	}
	if hidErr.Code != iossimdriver.HidClientErrTimeout {
		t.Fatalf("HidClientError.Code = %q, want %q", hidErr.Code, iossimdriver.HidClientErrTimeout)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("Tap() against a hung reply: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeToolTimeout, err)
	}
}

func TestHidClient_Dispatch_BeforeConnectReturnsNotConnectedError(t *testing.T) {
	client := iossimdriver.NewHidClient(&fakeDialer{}, iossimdriver.HidClientOptions{Port: 1})
	err := client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.5, Y: 0.5}, 50)
	var hidErr *iossimdriver.HidClientError
	if !errors.As(err, &hidErr) {
		t.Fatalf("Tap() error = %v (%T), want a *HidClientError", err, err)
	}
	if hidErr.Code != iossimdriver.HidClientErrNotConnected {
		t.Fatalf("HidClientError.Code = %q, want %q", hidErr.Code, iossimdriver.HidClientErrNotConnected)
	}
}

func TestHidClient_Dispose_FailsPendingDispatchesAsDisconnected(t *testing.T) {
	client, srv := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1, DispatchTimeout: 5 * time.Second})
	srv.setRespond(func(map[string]any) map[string]any { return nil })

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Tap(context.Background(), iossimdriver.NormalizedPoint{X: 0.5, Y: 0.5}, 50)
	}()
	// Give the dispatch a moment to register as pending before disposing.
	time.Sleep(20 * time.Millisecond)
	if err := client.Dispose(); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	select {
	case err := <-errCh:
		var hidErr *iossimdriver.HidClientError
		if !errors.As(err, &hidErr) {
			t.Fatalf("Tap() error = %v (%T), want a *HidClientError", err, err)
		}
		if hidErr.Code != iossimdriver.HidClientErrDisconnected {
			t.Fatalf("HidClientError.Code = %q, want %q", hidErr.Code, iossimdriver.HidClientErrDisconnected)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Tap() did not return after Dispose")
	}
}

func TestHidClient_Dispose_IsIdempotent(t *testing.T) {
	client, _ := newConnectedHidClient(t, iossimdriver.HidClientOptions{Port: 1})
	if err := client.Dispose(); err != nil {
		t.Fatalf("first Dispose() error = %v", err)
	}
	if err := client.Dispose(); err != nil {
		t.Fatalf("second Dispose() error = %v", err)
	}
}
