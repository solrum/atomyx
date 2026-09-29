package netcapture

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
)

func TestNewNilConfigReturnsNullCapture(t *testing.T) {
	nc, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	if _, ok := nc.(NullCapture); !ok {
		t.Fatalf("New(nil): got %T, want NullCapture", nc)
	}
}

func TestNewNoneTypeReturnsNullCapture(t *testing.T) {
	nc, err := New(&Config{Type: "none"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := nc.(NullCapture); !ok {
		t.Fatalf("New: got %T, want NullCapture", nc)
	}
}

func TestNewFileTypeRequiresPath(t *testing.T) {
	_, err := New(&Config{Type: "file"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Fatalf("error %q does not mention the missing path", err.Error())
	}
}

func TestNewFileTypeCreatesFileCapture(t *testing.T) {
	nc, err := New(&Config{Type: "file", Path: "/tmp/test.jsonl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := nc.(*FileCapture); !ok {
		t.Fatalf("New: got %T, want *FileCapture", nc)
	}
}

func TestNewMitmproxyTypeCreatesFileCapture(t *testing.T) {
	nc, err := New(&Config{Type: "mitmproxy", Path: "/tmp/addon.jsonl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := nc.(*FileCapture); !ok {
		t.Fatalf("New: got %T, want *FileCapture", nc)
	}
}

func TestNewMitmproxyTypeRequiresPath(t *testing.T) {
	_, err := New(&Config{Type: "mitmproxy"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Fatalf("error %q does not mention the missing path", err.Error())
	}
}

func TestNewUnknownTypeReturnsErrorWithAvailableList(t *testing.T) {
	_, err := New(&Config{Type: "unknown-proxy"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "available") {
		t.Fatalf("error %q does not list available types", err.Error())
	}
}

func TestRegisterAddsCustomAdapter(t *testing.T) {
	sentinel := NullCapture{}
	Register("netcapture-test-custom", func(Config) (runner.NetworkCapture, error) {
		return sentinel, nil
	})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, "netcapture-test-custom")
		registryMu.Unlock()
	})

	nc, err := New(&Config{Type: "netcapture-test-custom"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if nc != runner.NetworkCapture(sentinel) {
		t.Fatalf("New: got %#v, want the registered sentinel", nc)
	}
}

func TestFileCaptureFromFactoryImplementsNetworkCapture(t *testing.T) {
	nc, err := New(&Config{Type: "file", Path: "/tmp/factory-smoke.jsonl"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A nonexistent path is a legitimate initial state: Start loads
	// nothing and every subsequent call reports no captured traffic
	// until something writes to the path.
	if err := nc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if all := nc.All(); len(all) != 0 {
		t.Fatalf("All: got %v, want empty", all)
	}
	if err := nc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
