package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

// Transport is a newline-delimited JSON-RPC bridge over a reader and
// writer pair. It knows nothing about the host process. The caller
// passes in the streams (os.Stdin / os.Stdout in production, an
// io.Pipe in tests). It reads one JSON document per line, hands it to
// the Dispatcher, and writes the response back on its own line.
//
// Handlers and features never write to the output stream directly:
// they publish through an EventBus, and the composition root
// forwards each event through Emit, so a future change of encoding
// or channel stays contained to this type.
type Transport struct {
	in         *bufio.Reader
	out        io.Writer
	dispatcher *Dispatcher
	onError    func(err error)

	writeMu sync.Mutex
	wg      sync.WaitGroup
}

// NewTransport constructs a Transport reading requests from in and
// writing responses and events to out. onError, when non-nil, is
// called for a malformed line, a handler that could not be
// classified, or a write failure; a nil onError discards these.
func NewTransport(in io.Reader, out io.Writer, dispatcher *Dispatcher, onError func(err error)) *Transport {
	if onError == nil {
		onError = func(error) {}
	}
	return &Transport{
		in:         bufio.NewReader(in),
		out:        out,
		dispatcher: dispatcher,
		onError:    onError,
	}
}

// Run reads newline-delimited JSON from the input until it reaches
// EOF, a read error, or ctx is done. Each line is dispatched
// concurrently, mirroring a client that may have several requests
// in flight at once; Run does not return until every in-flight
// dispatch started during the call has finished writing its
// response. A blank line is ignored.
func (t *Transport) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		line, readErr := t.in.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed != "" {
			t.dispatchLine(ctx, trimmed)
		}
		if readErr != nil {
			break
		}
	}
	t.wg.Wait()
	return ctx.Err()
}

// Emit pushes event to the output stream on its own line.
func (t *Transport) Emit(event Event) {
	t.writeLine(event)
}

func (t *Transport) dispatchLine(ctx context.Context, line string) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.handleLine(ctx, line)
	}()
}

func (t *Transport) handleLine(ctx context.Context, line string) {
	req, err := ParseRequest([]byte(line))
	if err != nil {
		t.writeLine(requestParseFailure(err))
		return
	}
	t.writeLine(t.dispatcher.HandleRequest(ctx, req))
}

// requestParseFailure maps a ParseRequest failure to the fixed
// placeholder-id error response the wire contract uses when the
// input line carried no usable request id: "parse-error" for
// malformed JSON, "invalid-request" for JSON missing the required
// id/method fields.
func requestParseFailure(err error) Response {
	if errors.Is(err, errMalformedJSON) {
		return ErrorResponse("parse-error", ErrParseError, "Invalid JSON received", nil)
	}
	return ErrorResponse("invalid-request", ErrInvalidRequest, "Message missing required fields (id, method)", nil)
}

func (t *Transport) writeLine(payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.onError(err)
		return
	}
	encoded = append(encoded, '\n')

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.out.Write(encoded); err != nil {
		t.onError(err)
	}
}
