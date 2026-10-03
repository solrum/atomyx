package attach

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"dev.atomyx/internal/diag"
)

const (
	// ProtocolMajor is the highest major protocol version this build
	// speaks.
	ProtocolMajor = 1

	// MaxFrameBytes bounds one frame, newline included. It leaves room
	// for the base64 form of a large tablet screenshot.
	MaxFrameBytes = 32 << 20

	initialReadBytes = 64 << 10
)

// ErrFrameTooLarge reports a frame that does not fit in MaxFrameBytes.
// On the read side the stream cannot be resynchronized, so the
// connection must close. On the write side nothing was written, so the
// connection stays usable.
var ErrFrameTooLarge = errors.New("attach: frame exceeds the maximum frame size")

// ErrMalformedFrame reports a line that is not a JSON object. A
// FrameReader can still read the line after it, but a server answers it
// and closes the connection.
var ErrMalformedFrame = errors.New("attach: malformed frame")

// FrameKind tells what a Frame is.
type FrameKind int

const (
	KindRequest FrameKind = iota + 1
	KindResponse
	KindNotice
)

// Frame is the envelope of every line on the wire.
type Frame struct {
	V       int             `json:"v"`
	ID      int64           `json:"id,omitempty"`
	Type    string          `json:"type,omitempty"`
	Ok      *bool           `json:"ok,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Remedy  string          `json:"remedy,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Kind classifies the frame by which fields are set.
func (f Frame) Kind() FrameKind {
	switch {
	case f.Ok != nil:
		return KindResponse
	case f.ID > 0:
		return KindRequest
	default:
		return KindNotice
	}
}

// Decode unmarshals the frame's data into into. A frame without data
// leaves into untouched.
func (f Frame) Decode(into any) error {
	if len(f.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(f.Data, into); err != nil {
		return fmt.Errorf("attach: decoding %s data: %w", f.Type, err)
	}
	return nil
}

func marshalData(data any) (json.RawMessage, error) {
	if data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("attach: encoding data: %w", err)
	}
	return raw, nil
}

// NewRequest builds a request frame. A nil data leaves the data field
// out.
func NewRequest(id int64, messageType string, data any) (Frame, error) {
	raw, err := marshalData(data)
	if err != nil {
		return Frame{}, err
	}
	return Frame{V: ProtocolMajor, ID: id, Type: messageType, Data: raw}, nil
}

// NewResponse builds the success response to request id.
func NewResponse(id int64, messageType string, data any) (Frame, error) {
	raw, err := marshalData(data)
	if err != nil {
		return Frame{}, err
	}
	ok := true
	return Frame{V: ProtocolMajor, ID: id, Type: messageType, Ok: &ok, Data: raw}, nil
}

// NewErrorResponse builds the failure response to request id from err,
// carrying the code, message and remedy diag.Describe recovers.
func NewErrorResponse(id int64, messageType string, err error) Frame {
	code, message, remedy := diag.Describe(err)
	ok := false
	return Frame{V: ProtocolMajor, ID: id, Type: messageType, Ok: &ok, Code: code, Message: message, Remedy: remedy}
}

// NewNotice builds a notice frame.
func NewNotice(messageType string, data any) (Frame, error) {
	raw, err := marshalData(data)
	if err != nil {
		return Frame{}, err
	}
	return Frame{V: ProtocolMajor, Type: messageType, Data: raw}, nil
}

// EncodeFrame renders f as one line, newline included. It returns
// ErrFrameTooLarge when the line would pass MaxFrameBytes.
func EncodeFrame(f Frame) ([]byte, error) {
	line, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("attach: encoding frame: %w", err)
	}
	if len(line)+1 > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	return append(line, '\n'), nil
}

// WriteFrame writes f as one line with a single Write call, so
// concurrent writers that share a lock never interleave partial lines.
func WriteFrame(w io.Writer, f Frame) error {
	line, err := EncodeFrame(f)
	if err != nil {
		return err
	}
	_, err = w.Write(line)
	return err
}

// FrameReader reads frames from a stream. It is not safe for concurrent
// use.
type FrameReader struct {
	reader   *bufio.Reader
	maxBytes int
	// spill collects the part of a line that did not fit the reader's
	// buffer. It is dropped after each frame so a viewer that sent one
	// large frame does not keep its memory.
	spill []byte
	err   error
}

// NewFrameReader reads frames from r. The line buffer starts small and
// grows only while a line needs it, up to MaxFrameBytes, and it is
// released after each frame.
func NewFrameReader(r io.Reader) *FrameReader {
	return newFrameReader(r, MaxFrameBytes, initialReadBytes)
}

func newFrameReader(r io.Reader, maxBytes, chunkBytes int) *FrameReader {
	return &FrameReader{reader: bufio.NewReaderSize(r, max(chunkBytes, 16)), maxBytes: maxBytes}
}

// nextLine returns the next line with its newline. The result is valid
// until the next call.
func (r *FrameReader) nextLine() ([]byte, error) {
	r.spill = nil
	for {
		chunk, err := r.reader.ReadSlice('\n')
		if r.spill == nil && err == nil && len(chunk) <= r.maxBytes {
			return chunk, nil
		}
		if len(r.spill)+len(chunk) > r.maxBytes {
			return nil, ErrFrameTooLarge
		}
		r.spill = append(r.spill, chunk...)
		switch {
		case err == nil:
			return r.spill, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(r.spill) > 0:
			return r.spill, nil
		default:
			return nil, err
		}
	}
}

// ReadFrame returns the next frame. It returns io.EOF at the end of the
// stream, ErrFrameTooLarge for a line past MaxFrameBytes, and an error
// wrapping ErrMalformedFrame for a line that is not a JSON object.
// Blank lines are skipped. After ErrFrameTooLarge or a read error the
// stream cannot be resynchronized, so every later call returns the same
// error.
func (r *FrameReader) ReadFrame() (Frame, error) {
	if r.err != nil {
		return Frame{}, r.err
	}
	for {
		raw, err := r.nextLine()
		if err != nil {
			r.err = err
			r.spill = nil
			return Frame{}, err
		}
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var f Frame
		err = json.Unmarshal(line, &f)
		r.spill = nil
		if err != nil {
			return Frame{}, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
		}
		return f, nil
	}
}
