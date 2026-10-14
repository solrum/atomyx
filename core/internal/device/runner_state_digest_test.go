package device

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadRunnerState_RecordWithoutADigestParsesWithAnEmptyDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIM-1"+RunnerStateSuffix)
	legacy := `{"udid":"SIM-1","kind":"simulator","runnerId":"r1","hostPort":22087,"buildProductPath":"/p/app","buildProductModTime":"2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := ReadRunnerState(path)
	if err != nil {
		t.Fatalf("ReadRunnerState() error = %v", err)
	}

	product := state.BuildProduct()
	if product.Path != "/p/app" || product.Digest != "" || product.ModTime.IsZero() {
		t.Fatalf("BuildProduct() = %+v, want the path and stamp with no digest", product)
	}
}

func TestRunnerState_DigestRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIM-1"+RunnerStateSuffix)
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	written := RunnerState{UDID: "SIM-1", BuildProductPath: "/p/app", BuildProductModTime: stamp, BuildProductDigest: "abc123"}
	if err := WriteRunnerState(path, written); err != nil {
		t.Fatal(err)
	}

	state, err := ReadRunnerState(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := state.BuildProduct(); got.Digest != "abc123" || got.Path != "/p/app" {
		t.Fatalf("BuildProduct() = %+v, want the digest and path written", got)
	}
}
