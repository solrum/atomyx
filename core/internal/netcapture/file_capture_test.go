package netcapture

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/runner"
)

// fakeFileSystem is an in-memory FileSystem a test mutates directly to
// simulate a capture file being written, truncated, or rotated,
// without touching the real filesystem.
type fakeFileSystem struct {
	present bool
	content string
	readErr error
}

func (f *fakeFileSystem) Exists(string) bool { return f.present }

func (f *fakeFileSystem) ReadFile(string) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return []byte(f.content), nil
}

func linesOf(entries ...runner.CapturedRequest) string {
	var sb strings.Builder
	for _, e := range entries {
		raw := map[string]any{
			"method":    e.Method,
			"url":       e.URL,
			"status":    e.Status,
			"headers":   e.Headers,
			"body":      e.Body,
			"timestamp": e.TimestampMs,
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			panic(err)
		}
		sb.Write(encoded)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestFileCaptureReadsExistingEntries(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(runner.CapturedRequest{
		Method: "POST", URL: "https://api.test.com/transfer", Status: 200,
		Headers: map[string]string{}, Body: map[string]any{"ok": true}, TimestampMs: 1000,
	})}
	fc := newFileCaptureWithDeps("existing.jsonl", fs, clock.NewFakeClock(0))

	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	all := fc.All()
	if len(all) != 1 {
		t.Fatalf("All: got %d entries, want 1", len(all))
	}
	if all[0].Method != "POST" || all[0].Status != 200 {
		t.Fatalf("All[0]: got %+v", all[0])
	}
	if err := fc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestFileCaptureWaitForRequestMatchesMethodAndPath(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(
		runner.CapturedRequest{Method: "GET", URL: "https://api.test.com/users", Status: 200, Headers: map[string]string{}, Body: []any{}, TimestampMs: 1000},
		runner.CapturedRequest{Method: "POST", URL: "https://api.test.com/transfer", Status: 201, Headers: map[string]string{}, Body: map[string]any{"id": "tx-123"}, TimestampMs: 2000},
	)}
	fc := newFileCaptureWithDeps("match.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result, err := fc.WaitForRequest(context.Background(), "POST /transfer", 1000)
	if err != nil {
		t.Fatalf("WaitForRequest: %v", err)
	}
	if result.Status != 201 {
		t.Fatalf("Status: got %d, want 201", result.Status)
	}
	body, ok := result.Body.(map[string]any)
	if !ok || body["id"] != "tx-123" {
		t.Fatalf("Body: got %#v", result.Body)
	}
}

func TestFileCaptureWaitForRequestTimesOutWhenNoMatch(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(
		runner.CapturedRequest{Method: "GET", URL: "https://api.test.com/health", Status: 200, Headers: map[string]string{}, Body: nil, TimestampMs: 1000},
	)}
	fc := newFileCaptureWithDeps("nomatch.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := fc.WaitForRequest(context.Background(), "POST /transfer", 300)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "Timed out") {
		t.Fatalf("error %q does not mention a timeout", err.Error())
	}
}

func TestFileCaptureWaitForRequestReturnsMostRecentMatch(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(
		runner.CapturedRequest{Method: "POST", URL: "https://api.test.com/transfer", Status: 201, Headers: map[string]string{}, Body: nil, TimestampMs: 1000},
		runner.CapturedRequest{Method: "POST", URL: "https://api.test.com/transfer", Status: 202, Headers: map[string]string{}, Body: nil, TimestampMs: 2000},
	)}
	fc := newFileCaptureWithDeps("multi.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result, err := fc.WaitForRequest(context.Background(), "POST /transfer", 1000)
	if err != nil {
		t.Fatalf("WaitForRequest: %v", err)
	}
	if result.Status != 202 {
		t.Fatalf("Status: got %d, want 202 (the most recently captured match)", result.Status)
	}
}

func TestFileCaptureResetDropsTrailingUnreadRequest(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(
		runner.CapturedRequest{Method: "GET", URL: "https://api.test.com/health", Status: 200, Headers: map[string]string{}, Body: nil, TimestampMs: 1000},
	)}
	fc := newFileCaptureWithDeps("reset-trailing.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := len(fc.All()); got != 1 {
		t.Fatalf("All before append: got %d entries, want 1", got)
	}

	// Simulate the prior suite entry writing a request that this
	// instance has not yet read.
	fs.content += linesOf(runner.CapturedRequest{
		Method: "POST", URL: "https://api.test.com/from-previous-entry", Status: 200,
		Headers: map[string]string{}, Body: nil, TimestampMs: 2000,
	})

	fc.Reset()
	if all := fc.All(); len(all) != 0 {
		t.Fatalf("All after Reset: got %v, want empty", all)
	}

	fs.content += linesOf(runner.CapturedRequest{
		Method: "POST", URL: "https://api.test.com/from-next-entry", Status: 200,
		Headers: map[string]string{}, Body: nil, TimestampMs: 3000,
	})

	all := fc.All()
	if len(all) != 1 {
		t.Fatalf("All after next entry: got %d entries, want 1", len(all))
	}
	if all[0].URL != "https://api.test.com/from-next-entry" {
		t.Fatalf("All[0].URL: got %q", all[0].URL)
	}
}

func TestFileCaptureResumesAfterRotation(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: linesOf(runner.CapturedRequest{
		Method: "GET", URL: "https://api.test.com/before-rotation-padded-to-exceed-the-post-rotation-length", Status: 200,
		Headers: map[string]string{}, Body: nil, TimestampMs: 1000,
	})}
	fc := newFileCaptureWithDeps("rotate.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := len(fc.All()); got != 1 {
		t.Fatalf("All before rotation: got %d entries, want 1", got)
	}

	fc.Reset()

	// Simulate log rotation: the file is truncated then rewritten with
	// content shorter than the offset this instance already read past.
	fs.content = ""
	fs.content = linesOf(runner.CapturedRequest{
		Method: "POST", URL: "https://api.test.com/x", Status: 200,
		Headers: map[string]string{}, Body: nil, TimestampMs: 4000,
	})

	all := fc.All()
	if len(all) != 1 {
		t.Fatalf("All after rotation: got %d entries, want 1", len(all))
	}
	if all[0].URL != "https://api.test.com/x" {
		t.Fatalf("All[0].URL: got %q", all[0].URL)
	}
}

func TestFileCaptureWaitForRequestDistinguishesAGenuineReadErrorFromAMissingFile(t *testing.T) {
	fs := &fakeFileSystem{present: true, readErr: errors.New("permission denied")}
	fc := newFileCaptureWithDeps("unreadable.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := fc.WaitForRequest(context.Background(), "POST /transfer", 300)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error %q does not surface the underlying read failure", err.Error())
	}
	if strings.Contains(err.Error(), "Captured 0 requests total") {
		t.Fatalf("error %q reports a genuine read failure as if the file were simply empty", err.Error())
	}
}

func TestFileCaptureAllReturnsEmptyWhenFileMissing(t *testing.T) {
	fs := &fakeFileSystem{present: false}
	fc := newFileCaptureWithDeps("missing.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if all := fc.All(); len(all) != 0 {
		t.Fatalf("All: got %v, want empty", all)
	}
}

func TestFileCaptureSkipsMalformedLines(t *testing.T) {
	fs := &fakeFileSystem{present: true, content: strings.Join([]string{
		"not json",
		`{"method":"GET","url":"https://api.test.com/no-status"}`,
		`{"method":"GET","status":200}`,
		`{"method":"","url":"https://api.test.com/empty-method","status":200}`,
		`{"method":"GET","url":"https://api.test.com/ok","status":200,"headers":{},"body":null,"timestamp":1000}`,
		"",
	}, "\n")}
	fc := newFileCaptureWithDeps("malformed.jsonl", fs, clock.NewFakeClock(0))
	if err := fc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	all := fc.All()
	if len(all) != 1 {
		t.Fatalf("All: got %d entries, want 1 (only the well-formed line): %+v", len(all), all)
	}
	if all[0].URL != "https://api.test.com/ok" {
		t.Fatalf("All[0].URL: got %q", all[0].URL)
	}
}

func TestParseCapturePattern(t *testing.T) {
	cases := []struct {
		pattern    string
		wantMethod string
		wantPath   string
	}{
		{"POST /api/transfer", "POST", "/api/transfer"},
		{"get /users", "GET", "/users"},
		{"/users", "", "/users"},
		{"api.test.com/users", "", "api.test.com/users"},
		{"GETX /users", "", "GETX /users"},
	}
	for _, c := range cases {
		method, path := parseCapturePattern(c.pattern)
		if method != c.wantMethod || path != c.wantPath {
			t.Errorf("parseCapturePattern(%q): got (%q, %q), want (%q, %q)",
				c.pattern, method, path, c.wantMethod, c.wantPath)
		}
	}
}

func TestMatchesCapturePath(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		pattern string
		want    bool
	}{
		{"exact path match", "https://api.test.com/transfer", "/transfer", true},
		{"exact path mismatch", "https://api.test.com/transfer", "/transfers", false},
		{"prefix wildcard match", "https://api.test.com/users/42", "/users/*", true},
		{"prefix wildcard mismatch", "https://api.test.com/orders/42", "/users/*", false},
		{"substring match on non-slash pattern", "https://api.test.com/users/42", "api.test.com", true},
		{"non-absolute url falls back to substring", "/transfer", "/transfer", true},
		{"non-absolute url substring mismatch", "/transfer", "/other", false},
	}
	for _, c := range cases {
		if got := matchesCapturePath(c.url, c.pattern); got != c.want {
			t.Errorf("%s: matchesCapturePath(%q, %q) = %v, want %v", c.name, c.url, c.pattern, got, c.want)
		}
	}
}
