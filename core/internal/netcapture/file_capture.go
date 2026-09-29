package netcapture

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/runner"
)

// capturePollIntervalMs is the poll cadence FileCapture uses both to
// refresh its buffer from disk and to re-check a pending
// WaitForRequest. Short enough to keep script perception latency low;
// long enough to avoid hammering the capture file's inode.
const capturePollIntervalMs = 100

// FileSystem is the filesystem seam FileCapture reads through:
// whether the capture file currently exists, and its full contents.
// Injecting it lets a test drive FileCapture's rotation-recovery and
// matching logic against an in-memory fake instead of the real
// filesystem.
type FileSystem interface {
	Exists(path string) bool
	ReadFile(path string) ([]byte, error)
}

// OSFileSystem is the production FileSystem, backed by the host's
// filesystem.
type OSFileSystem struct{}

func (OSFileSystem) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (OSFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// FileCapture is a runner.NetworkCapture adapter that reads HTTP
// traffic recorded as JSON-lines in a file: one JSON object per line,
// each matching the CapturedRequest shape. It is the generic adapter
// behind both the "file" and "mitmproxy" Config types. A mitmproxy
// addon writes to this format natively, and any other proxy tool that
// can emit JSON-lines works the same way.
//
// Line format:
//
//	{"method":"POST","url":"https://api.example.com/transfer","status":200,"headers":{},"body":{"ok":true},"timestamp":1713300000000}
type FileCapture struct {
	fs       FileSystem
	clock    clock.Clock
	filePath string

	requests []runner.CapturedRequest
	lastSize int
	// lastReadErr holds the error from the most recent failed read of
	// filePath, or nil when the file does not exist or the last read
	// succeeded. Kept separately from the buffer so a genuine read
	// failure (permission denied, a transient I/O error) can be
	// surfaced to a caller waiting on a match instead of looking
	// identical to a capture file that legitimately has nothing new to
	// report.
	lastReadErr error
}

var _ runner.NetworkCapture = (*FileCapture)(nil)

// NewFileCapture constructs a FileCapture reading from filePath on
// the host filesystem.
func NewFileCapture(filePath string) *FileCapture {
	return newFileCaptureWithDeps(filePath, OSFileSystem{}, clock.SystemClock{})
}

func newFileCaptureWithDeps(filePath string, fs FileSystem, clk clock.Clock) *FileCapture {
	return &FileCapture{filePath: filePath, fs: fs, clock: clk}
}

// Start loads whatever the capture file already holds. Nothing else
// reads the buffer without first refreshing it itself (All and
// WaitForRequest both do), so no background poll is needed to keep it
// current between calls.
func (f *FileCapture) Start(context.Context) error {
	f.loadExisting()
	return nil
}

func (f *FileCapture) Stop(context.Context) error { return nil }

// WaitForRequest polls the capture file for a request matching
// pattern ("METHOD /path", or "/path" to match any method) up to
// timeoutMs, returning the most recently captured match. It returns
// an error once the deadline passes with no match.
func (f *FileCapture) WaitForRequest(ctx context.Context, pattern string, timeoutMs int) (runner.CapturedRequest, error) {
	method, path := parseCapturePattern(pattern)
	deadline := f.clock.NowMs() + int64(timeoutMs)

	for f.clock.NowMs() < deadline {
		f.loadNew()
		if match, ok := f.findMatch(method, path); ok {
			return match, nil
		}
		if err := f.clock.Sleep(ctx, capturePollIntervalMs); err != nil {
			return runner.CapturedRequest{}, err
		}
	}

	if f.lastReadErr != nil {
		return runner.CapturedRequest{}, fmt.Errorf(
			"timed out waiting for %q after %dms: the capture file could not be read: %w",
			pattern, timeoutMs, f.lastReadErr)
	}

	//nolint:staticcheck // the leading capital in "Timed out" is asserted verbatim by a test distinguishing this timeout path from the lastReadErr path above
	return runner.CapturedRequest{}, fmt.Errorf(
		"Timed out waiting for %q after %dms, captured %d requests total, "+
			"check that the proxy is running and the app routes through it",
		pattern, timeoutMs, len(f.requests))
}

func (f *FileCapture) All() []runner.CapturedRequest {
	f.loadNew()
	out := make([]runner.CapturedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// Reset discards the buffer and anchors the read offset to the file's
// length at reset time, not just clearing the buffer. A request the
// writer flushed to disk before this call but that Reset's caller had
// not yet observed would otherwise still sit past the old offset and
// leak into the next read as if it arrived after the reset.
func (f *FileCapture) Reset() {
	f.requests = nil
	f.lastSize = f.currentFileLength()
}

func (f *FileCapture) loadExisting() {
	content, ok := f.readFile()
	if !ok {
		f.requests = nil
		f.lastSize = 0
		return
	}
	f.lastSize = len(content)
	f.requests = parseCaptureLines(content)
}

func (f *FileCapture) loadNew() {
	content, ok := f.readFile()
	if !ok {
		return
	}
	if len(content) < f.lastSize {
		// The file is shorter than the last read offset, which happens
		// when the capture file is truncated or rotated between suite
		// entries. Treat everything currently on disk as unread instead
		// of returning early, or capture would silently stop forever
		// once a rotation drops the file below the old offset.
		f.lastSize = 0
	}
	if len(content) <= f.lastSize {
		return
	}
	newPart := content[f.lastSize:]
	f.lastSize = len(content)
	f.requests = append(f.requests, parseCaptureLines(newPart)...)
}

func (f *FileCapture) currentFileLength() int {
	content, ok := f.readFile()
	if !ok {
		return 0
	}
	return len(content)
}

func (f *FileCapture) readFile() ([]byte, bool) {
	if !f.fs.Exists(f.filePath) {
		f.lastReadErr = nil
		return nil, false
	}
	content, err := f.fs.ReadFile(f.filePath)
	if err != nil {
		f.lastReadErr = err
		return nil, false
	}
	f.lastReadErr = nil
	return content, true
}

// findMatch scans the buffer newest-first so a pattern that matches
// more than one recorded request resolves to the one most likely
// relevant to the step waiting on it.
func (f *FileCapture) findMatch(method, path string) (runner.CapturedRequest, bool) {
	for i := len(f.requests) - 1; i >= 0; i-- {
		req := f.requests[i]
		if method != "" && !strings.EqualFold(req.Method, method) {
			continue
		}
		if matchesCapturePath(req.URL, path) {
			return req, true
		}
	}
	return runner.CapturedRequest{}, false
}

var capturePatternRe = regexp.MustCompile(`(?i)^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(.+)$`)

// parseCapturePattern splits a capture pattern like "POST /api/transfer"
// into a method and a path. method is "" when pattern carries no
// method prefix, matching any method.
func parseCapturePattern(pattern string) (method string, path string) {
	if m := capturePatternRe.FindStringSubmatch(pattern); m != nil {
		return strings.ToUpper(m[1]), strings.TrimSpace(m[2])
	}
	return "", strings.TrimSpace(pattern)
}

// matchesCapturePath reports whether rawURL matches pattern. A
// pattern starting with "/" matches against the URL's path component
// (a trailing "*" makes it a prefix match, otherwise it must match
// exactly); any other pattern is matched as a substring of the whole
// URL. rawURL that isn't parseable as an absolute URL (no scheme or
// no host) falls back to the substring match too, mirroring the
// JavaScript URL constructor's requirement of an absolute URL where
// Go's net/url.Parse is otherwise permissive of a bare path.
func matchesCapturePath(rawURL, pattern string) bool {
	urlPath, ok := capturePathOf(rawURL)
	if !ok {
		return strings.Contains(rawURL, pattern)
	}
	if strings.HasPrefix(pattern, "/") {
		if strings.HasSuffix(pattern, "*") {
			return strings.HasPrefix(urlPath, strings.TrimSuffix(pattern, "*"))
		}
		return urlPath == pattern
	}
	return strings.Contains(rawURL, pattern)
}

func capturePathOf(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	return parsed.Path, true
}

// rawCapturedRequest is one JSON-lines record before it is known to
// carry every field CapturedRequest requires. Status and Timestamp
// are pointers so a missing field is distinguishable from an
// explicit zero.
type rawCapturedRequest struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Status    *float64          `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body"`
	Timestamp *float64          `json:"timestamp"`
}

// parseCaptureLines decodes content as JSON-lines, keeping only lines
// that parse as a JSON object carrying a non-empty method, a non-empty
// url, and a numeric status. Every other line, including malformed JSON, a
// blank line, or a record missing a required field, is skipped rather
// than failing the whole read, since a capture file is written
// incrementally and a reader may observe a line mid-write.
func parseCaptureLines(content []byte) []runner.CapturedRequest {
	var out []runner.CapturedRequest
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var raw rawCapturedRequest
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			continue
		}
		if raw.Method == "" || raw.URL == "" || raw.Status == nil {
			continue
		}
		var timestampMs int64
		if raw.Timestamp != nil {
			timestampMs = int64(*raw.Timestamp)
		}
		out = append(out, runner.CapturedRequest{
			Method:      raw.Method,
			URL:         raw.URL,
			Status:      int(*raw.Status),
			Headers:     raw.Headers,
			Body:        raw.Body,
			TimestampMs: timestampMs,
		})
	}
	return out
}
