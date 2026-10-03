package attach

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func openFDs(t *testing.T) int {
	t.Helper()
	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

// waitForNoLeak polls on a channel until the goroutine and descriptor
// counts are back to their baselines, because a goroutine that has
// finished its work may still be counted for a moment.
func waitForNoLeak(t *testing.T, baseGoroutines, baseFDs int) {
	t.Helper()
	const attempts = 500
	for range attempts {
		if runtime.NumGoroutine() <= baseGoroutines && openFDs(t) <= baseFDs {
			return
		}
		<-time.After(10 * time.Millisecond)
	}
	t.Fatalf("leak: %d goroutines (base %d), %d descriptors (base %d)",
		runtime.NumGoroutine(), baseGoroutines, openFDs(t), baseFDs)
}
