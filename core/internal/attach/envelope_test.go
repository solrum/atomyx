package attach

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

type repeatReader struct{ b byte }

func (r repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

func mustFrame(t *testing.T, f Frame, err error) Frame {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFramesRoundTripAndClassify(t *testing.T) {
	req, err := NewRequest(7, TypeSubscribe, Subscribe{SinceSeq: 3})
	resp, err2 := NewResponse(7, TypeSubscribe, nil)
	notice, err3 := NewNotice(NoticeTree, TreeNotice{Revision: 2, Epoch: 1})
	frames := []Frame{
		mustFrame(t, req, err),
		mustFrame(t, resp, err2),
		mustFrame(t, notice, err3),
		NewErrorResponse(8, "x.y", diag.NewUnsupported("x.y")),
	}
	wantKinds := []FrameKind{KindRequest, KindResponse, KindNotice, KindResponse}

	var buf bytes.Buffer
	for _, f := range frames {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatal(err)
		}
	}
	reader := NewFrameReader(&buf)
	for i := range frames {
		got, err := reader.ReadFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if got.Kind() != wantKinds[i] {
			t.Fatalf("frame %d kind = %d, want %d", i, got.Kind(), wantKinds[i])
		}
		if got.Type != frames[i].Type || got.ID != frames[i].ID {
			t.Fatalf("frame %d = %+v, want %+v", i, got, frames[i])
		}
	}
	if _, err := reader.ReadFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("end of stream error = %v, want io.EOF", err)
	}
}

func TestUnknownTypeResponseKeepsTheStreamUsable(t *testing.T) {
	var buf bytes.Buffer
	unsupported := NewErrorResponse(1, "control.borrow", diag.NewUnsupported("control.borrow"))
	status, err := NewResponse(2, TypeStatus, Status{State: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []Frame{unsupported, status} {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatal(err)
		}
	}

	reader := NewFrameReader(&buf)
	first, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if first.Ok == nil || *first.Ok || first.Code != diag.CodeUnsupported || first.Remedy == "" {
		t.Fatalf("first = %+v, want ok false with code UNSUPPORTED and a remedy", first)
	}
	second, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("frame after UNSUPPORTED: %v", err)
	}
	var got Status
	if err := second.Decode(&got); err != nil || got.State != "idle" {
		t.Fatalf("status = %+v, err %v", got, err)
	}
}

func TestNegotiateVersion(t *testing.T) {
	chosen, err := NegotiateVersion(VersionRange{Min: 1, Max: 3}, VersionRange{Min: 1, Max: 2})
	if err != nil || chosen != 2 {
		t.Fatalf("overlap = %d, %v, want 2", chosen, err)
	}

	for name, client := range map[string]VersionRange{
		"above": {Min: 2, Max: 3},
		"below": {Min: 0, Max: 0},
		"empty": {Min: 3, Max: 1},
	} {
		_, err := NegotiateVersion(client, SupportedVersions)
		if code, _, _ := diag.Describe(err); code != diag.CodeUnsupportedVersion {
			t.Fatalf("%s: code = %q, want %s", name, code, diag.CodeUnsupportedVersion)
		}
	}
}

func TestReadOversizedFrameReturnsNamedError(t *testing.T) {
	reader := NewFrameReader(io.LimitReader(repeatReader{'a'}, MaxFrameBytes+16))
	if _, err := reader.ReadFrame(); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("error = %v, want ErrFrameTooLarge", err)
	}
}

func TestWriteOversizedFrameWritesNothing(t *testing.T) {
	big := json.RawMessage(`"` + strings.Repeat("a", MaxFrameBytes) + `"`)
	var buf bytes.Buffer
	err := WriteFrame(&buf, Frame{V: 1, ID: 1, Type: "x", Data: big})
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("error = %v, want ErrFrameTooLarge", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %d bytes of an oversized frame", buf.Len())
	}
	if err := WriteFrame(&buf, NewErrorResponse(1, "x", diag.NewUnsupported("x"))); err != nil {
		t.Fatalf("writer unusable after refusal: %v", err)
	}
}

func TestMalformedLineIsReportedAndSkipped(t *testing.T) {
	reader := NewFrameReader(strings.NewReader("not json\n\n" + `{"v":1,"id":1,"type":"session.bye"}` + "\n"))
	if _, err := reader.ReadFrame(); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("error = %v, want ErrMalformedFrame", err)
	}
	next, err := reader.ReadFrame()
	if err != nil || next.Type != TypeBye {
		t.Fatalf("next = %+v, %v, want session.bye", next, err)
	}
}
