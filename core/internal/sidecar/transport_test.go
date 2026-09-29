package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func outputLines(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()
	trimmed := strings.TrimRight(out.String(), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestTransport_Run_ValidRequest_WritesSuccessResponseLine(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	})
	in := strings.NewReader(`{"id":"1","method":"ping"}` + "\n")
	var out bytes.Buffer

	tr := NewTransport(in, &out, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1 (got %q)", len(lines), out.String())
	}
	if lines[0] != `{"id":"1","result":"pong"}` {
		t.Fatalf("output = %s, want the success response for ping", lines[0])
	}
}

func TestTransport_Run_MalformedJSON_WritesParseErrorWithFixedID(t *testing.T) {
	d := NewDispatcher()
	in := strings.NewReader(`{not json` + "\n")
	var out bytes.Buffer

	tr := NewTransport(in, &out, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1 (got %q)", len(lines), out.String())
	}
	want := `{"id":"parse-error","error":{"code":"ParseError","message":"Invalid JSON received"}}`
	if lines[0] != want {
		t.Fatalf("output = %s, want %s", lines[0], want)
	}
}

func TestTransport_Run_MissingRequiredFields_WritesInvalidRequestWithFixedID(t *testing.T) {
	d := NewDispatcher()
	in := strings.NewReader(`{"method":"ping"}` + "\n")
	var out bytes.Buffer

	tr := NewTransport(in, &out, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1 (got %q)", len(lines), out.String())
	}
	want := `{"id":"invalid-request","error":{"code":"InvalidRequest","message":"Message missing required fields (id, method)"}}`
	if lines[0] != want {
		t.Fatalf("output = %s, want %s", lines[0], want)
	}
}

func TestTransport_Run_BlankLines_AreIgnored(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	})
	in := strings.NewReader("\n" + `{"id":"1","method":"ping"}` + "\n\n")
	var out bytes.Buffer

	tr := NewTransport(in, &out, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1, blank lines must not produce a response (got %q)", len(lines), out.String())
	}
}

func TestTransport_Run_UnregisteredMethod_WritesMethodNotFoundResponse(t *testing.T) {
	d := NewDispatcher()
	in := strings.NewReader(`{"id":"1","method":"missing"}` + "\n")
	var out bytes.Buffer

	tr := NewTransport(in, &out, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1 (got %q)", len(lines), out.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("output line is not valid JSON: %v", err)
	}
	errObj, ok := decoded["error"].(map[string]any)
	if !ok || errObj["code"] != "MethodNotFound" {
		t.Fatalf("output = %s, want error.code = MethodNotFound", lines[0])
	}
}

func TestTransport_Emit_WritesEventLine(t *testing.T) {
	d := NewDispatcher()
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(""), &out, d, nil)

	tr.Emit(Event{Event: "device.connected", Payload: map[string]any{"id": "emulator-5554"}})

	lines := outputLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("output lines = %d, want 1 (got %q)", len(lines), out.String())
	}
	want := `{"event":"device.connected","payload":{"id":"emulator-5554"}}`
	if lines[0] != want {
		t.Fatalf("output = %s, want %s", lines[0], want)
	}
}

func TestTransport_Run_OnEOF_ReturnsNilError(t *testing.T) {
	d := NewDispatcher()
	tr := NewTransport(strings.NewReader(""), &bytes.Buffer{}, d, nil)
	if err := tr.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil on a closed/empty input", err)
	}
}

// TestTransport_Run_ShutdownDrainsInFlightDispatchBeforeReturning pins
// the mechanism a caller's shutdown path depends on: cancelling ctx
// alone can never unblock a Run loop parked in a blocking read on its
// input, so the caller must also close that input to make the read
// return. Once it does, Run still waits for every dispatch already in
// flight to finish writing its response before Run itself returns.
func TestTransport_Run_ShutdownDrainsInFlightDispatchBeforeReturning(t *testing.T) {
	d := NewDispatcher()
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	_ = d.Register("slow", func(context.Context, json.RawMessage) (any, error) {
		close(handlerStarted)
		<-releaseHandler
		return "done", nil
	})

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer

	tr := NewTransport(pr, &out, d, nil)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- tr.Run(ctx) }()

	if _, err := pw.Write([]byte(`{"id":"1","method":"slow"}` + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatched handler never started")
	}

	cancel()
	select {
	case <-runDone:
		t.Fatal("Run() returned on ctx cancellation alone, without its blocked read on input ever unblocking")
	case <-time.After(100 * time.Millisecond):
	}

	// Mirrors the shutdown path's fix: closing the input is what
	// unblocks the pending read, not ctx cancellation.
	_ = pr.Close()
	select {
	case <-runDone:
		t.Fatal("Run() returned before the in-flight dispatch finished writing its response")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseHandler)
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after the in-flight dispatch finished")
	}

	lines := outputLines(t, &out)
	if len(lines) != 1 || lines[0] != `{"id":"1","result":"done"}` {
		t.Fatalf("output = %q, want the slow handler's response written before Run() returned", out.String())
	}
}
