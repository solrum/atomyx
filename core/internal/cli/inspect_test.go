package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/shutdown"
)

const inspectIdleMs = 1_000

// lineSink is a goroutine-safe writer that announces every complete
// line, so a test waits for output instead of sleeping.
type lineSink struct {
	mu      sync.Mutex
	partial string
	lines   chan string
}

func newLineSink() *lineSink { return &lineSink{lines: make(chan string, 256)} }

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial += string(p)
	for {
		i := strings.IndexByte(s.partial, '\n')
		if i < 0 {
			return len(p), nil
		}
		s.lines <- s.partial[:i]
		s.partial = s.partial[i+1:]
	}
}

// awaitLine returns the first line that contains want.
func (s *lineSink) awaitLine(t *testing.T, want string) string {
	t.Helper()
	for line := range s.lines {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("output ended without a line containing %q", want)
	return ""
}

type fakeBrowser struct {
	mu   sync.Mutex
	urls []string
}

func (b *fakeBrowser) Open(url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.urls = append(b.urls, url)
	return nil
}

func (b *fakeBrowser) opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.urls...)
}

// inspectRun is one running Inspect command.
type inspectRun struct {
	deps    Deps
	stdout  *lineSink
	stderr  *lineSink
	browser *fakeBrowser
	clk     *clock.ManualClock
	manager *lease.Manager
	cancel  context.CancelCauseFunc
	exit    chan int
}

func startInspect(t *testing.T, manager *lease.Manager, flags map[string]FlagValue, stdin io.Reader) *inspectRun {
	t.Helper()
	run := &inspectRun{stdout: newLineSink(), stderr: newLineSink(), browser: &fakeBrowser{}, clk: clock.NewManualClock(1), manager: manager, exit: make(chan int, 1)}
	run.deps = leaseDeps(t, manager, nil, nil)
	run.deps.Stdout, run.deps.Stderr = run.stdout, run.stderr
	run.deps.Browser, run.deps.Stdin, run.deps.Clock = run.browser, stdin, run.clk
	run.deps.Timeouts.McpIdleMs = inspectIdleMs
	ctx, cancel := context.WithCancelCause(context.Background())
	run.cancel = cancel
	t.Cleanup(func() { cancel(nil) })
	go func() { run.exit <- Inspect(ctx, run.deps, flags) }()
	return run
}

func (r *inspectRun) stop(sig syscall.Signal) int {
	r.cancel(shutdown.InterruptCause(sig))
	return <-r.exit
}

func (r *inspectRun) otherProcessOpens(t *testing.T) error {
	t.Helper()
	other, err := openDevice(context.Background(), leaseDeps(t, r.manager, nil, nil), "atomyx run", device.Descriptor{Platform: device.PlatformAndroid, ID: "emulator-5554"})
	if err == nil {
		other.Close()
	}
	return err
}

type inspectClient struct {
	base, token string
}

func connectClient(t *testing.T, url string) *inspectClient {
	t.Helper()
	hash := strings.IndexByte(url, '#')
	c := &inspectClient{base: strings.TrimSuffix(url[:hash], "/")}
	var out struct{ Token string }
	status := c.send(t, http.MethodPost, "/api/v1/auth", `{"code":"`+url[hash+1:]+`"}`, &out)
	if status != http.StatusOK || out.Token == "" {
		t.Fatalf("auth = %d, token %q", status, out.Token)
	}
	c.token = out.Token
	return c
}

func (c *inspectClient) send(t *testing.T, method, path, body string, into any) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", c.base)
	req.Header.Set("X-Atomyx-Token", c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s %s: body %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (c *inspectClient) state(t *testing.T) string {
	t.Helper()
	var info struct{ State string }
	if status := c.send(t, http.MethodGet, "/api/v1/session", "", &info); status != http.StatusOK {
		t.Fatalf("session = %d", status)
	}
	return info.State
}

func TestInspect_PrintsALoopbackURLWithTheCodeInTheFragmentAndOpensItOnce(t *testing.T) {
	run := startInspect(t, newLeaseManager(t), heldDevice, nil)
	url := run.stdout.awaitLine(t, "http://")
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.Contains(url, "/#") || strings.Contains(url, "localhost") {
		t.Fatalf("url = %q, want a 127.0.0.1 URL with a fragment", url)
	}
	if got := run.browser.opened(); len(got) != 1 || got[0] != url {
		t.Fatalf("opened = %v, want exactly %q", got, url)
	}
	if code := run.stop(syscall.SIGINT); code != shutdown.ExitCodeSigint {
		t.Fatalf("exit = %d", code)
	}
}

func TestInspect_NoOpenPrintsTheURLAndLeavesTheBrowserAlone(t *testing.T) {
	flags := map[string]FlagValue{"--platform": "android", "--device": "emulator-5554", "--no-open": true}
	run := startInspect(t, newLeaseManager(t), flags, nil)
	run.stdout.awaitLine(t, "http://")
	run.stop(syscall.SIGINT)
	if got := run.browser.opened(); len(got) != 0 {
		t.Fatalf("opened = %v, want none", got)
	}
}

func TestInspect_EnterMintsAFreshURLAndEndOfInputKeepsServing(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	run := startInspect(t, newLeaseManager(t), heldDevice, reader)
	first := run.stdout.awaitLine(t, "http://")

	if _, err := io.WriteString(writer, "\n"); err != nil {
		t.Fatal(err)
	}
	second := run.stdout.awaitLine(t, "http://")
	if second == first {
		t.Fatal("Enter printed the same URL again")
	}
	if got := run.browser.opened(); len(got) != 2 || got[1] != second {
		t.Fatalf("opened = %v, want the second URL opened", got)
	}
	_ = writer.Close()
	client := connectClient(t, second)
	if state := client.state(t); state != "idle" {
		t.Fatalf("state = %q after the end of input, want a live idle session", state)
	}
	run.stop(syscall.SIGINT)
}

func TestInspect_AStopSignalReleasesTheDeviceAndExitsWithTheSignalCode(t *testing.T) {
	for sig, want := range map[syscall.Signal]int{syscall.SIGINT: shutdown.ExitCodeSigint, syscall.SIGTERM: shutdown.ExitCodeSigterm} {
		run := startInspect(t, newLeaseManager(t), heldDevice, nil)
		run.stdout.awaitLine(t, "http://")
		if err := run.otherProcessOpens(t); err == nil {
			t.Fatalf("%v: a second process opened the held device", sig)
		}
		if code := run.stop(sig); code != want {
			t.Fatalf("%v: exit = %d, want %d", sig, code, want)
		}
		if err := run.otherProcessOpens(t); err != nil {
			t.Fatalf("%v: the device was not released: %v", sig, err)
		}
	}
}

func TestInspect_AHeldDeviceFailsWithItsCodeAndPrintsNoURL(t *testing.T) {
	manager := newLeaseManager(t)
	holdDevice(t, manager)
	run := startInspect(t, manager, heldDevice, nil)
	if code := <-run.exit; code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	run.stderr.awaitLine(t, diag.CodeDeviceHeld)
	select {
	case line := <-run.stdout.lines:
		t.Fatalf("stdout printed %q for a device that was never opened", line)
	default:
	}
}

func TestInspect_IdleReleaseFreesTheDeviceAndReconnectTakesItAgain(t *testing.T) {
	run := startInspect(t, newLeaseManager(t), heldDevice, nil)
	client := connectClient(t, run.stdout.awaitLine(t, "http://"))
	run.clk.AwaitSleepers(1)

	if err := run.otherProcessOpens(t); err == nil {
		t.Fatal("a second process opened the device before the idle release")
	}
	run.clk.Advance(inspectIdleMs)
	run.stderr.awaitLine(t, "inspector.idle.released")
	if state := client.state(t); state != "released" {
		t.Fatalf("state = %q, want released", state)
	}
	if err := run.otherProcessOpens(t); err != nil {
		t.Fatalf("the device was not free after the release: %v", err)
	}

	if status := client.send(t, http.MethodPost, "/api/v1/reconnect", "", nil); status != http.StatusOK {
		t.Fatalf("reconnect = %d, want 200", status)
	}
	if state := client.state(t); state != "idle" {
		t.Fatalf("state = %q after reconnect, want idle", state)
	}
	if err := run.otherProcessOpens(t); err == nil {
		t.Fatal("a second process opened the device after the reconnect")
	}
	run.stop(syscall.SIGINT)
}

func TestInspect_AnUnknownPlatformIsAUsageError(t *testing.T) {
	run := startInspect(t, newLeaseManager(t), map[string]FlagValue{"--platform": "windows"}, nil)
	if code := <-run.exit; code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
