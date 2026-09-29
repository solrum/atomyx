package iossimdriver

import (
	"context"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"dev.atomyx/internal/process"
)

// requiredXcodeMajor is the minimum Xcode major version verified to
// expose the SimDeviceLegacyHIDClient and IndigoHIDClient symbols the
// atomyx-sim-hid helper needs. The helper additionally probes each
// symbol at load time and degrades gracefully if any are missing.
const requiredXcodeMajor = 16

var simDirectCache struct {
	mu  sync.Mutex
	set bool
	val bool
}

// ResetSimDirectCacheForTests clears the memoized IsSimDirectSupported
// result so a test observes a fresh probe.
func ResetSimDirectCacheForTests() {
	simDirectCache.mu.Lock()
	defer simDirectCache.mu.Unlock()
	simDirectCache.set = false
}

// IsSimDirectSupported reports whether the current host can run the
// direct-HID path: an arm64 CPU (the SimDeviceLegacyHIDClient private
// framework is only present on arm64 macOS builds of Xcode) and an
// installed Xcode whose major version meets requiredXcodeMajor. The
// result is memoized for the process lifetime.
//
// Returning true means the system COULD run the HID path; the caller
// (the device factory) still decides whether to actually take it, and
// still has to start the atomyx-sim-hid helper and obtain its
// handshake port.
//
// Any failure probing the host (Xcode not installed, xcode-select
// absent, a malformed plist) reports false, the safe fallback that
// preserves the XCUITest path.
func IsSimDirectSupported(ctx context.Context, proc process.Runner) bool {
	simDirectCache.mu.Lock()
	defer simDirectCache.mu.Unlock()
	if simDirectCache.set {
		return simDirectCache.val
	}
	simDirectCache.val = checkSimDirectSupported(ctx, proc)
	simDirectCache.set = true
	return simDirectCache.val
}

func checkSimDirectSupported(ctx context.Context, proc process.Runner) bool {
	if runtime.GOARCH != "arm64" {
		return false
	}

	devResult, err := proc.Run(ctx, "xcode-select", "-p")
	if err != nil {
		return false
	}
	developerDir := strings.TrimSpace(devResult.Stdout)
	if developerDir == "" {
		return false
	}

	// Xcode's Info.plist is binary on shipping builds; plutil ships
	// with macOS and reads both formats, and its "raw" output is just
	// the extracted value.
	plistPath := filepath.Join(developerDir, "..", "Info.plist")
	plistResult, err := proc.Run(ctx, "plutil", "-extract", "DTXcode", "raw", plistPath)
	if err != nil {
		return false
	}
	dtxcodeStr := strings.TrimSpace(plistResult.Stdout)
	if dtxcodeStr == "" {
		return false
	}

	// DTXcode is a zero-padded decimal: "1620" means 16.2, "2600"
	// means 26.0. Dividing by 100 and flooring gives the major
	// version.
	dtxcode, err := strconv.Atoi(dtxcodeStr)
	if err != nil {
		return false
	}
	majorVersion := dtxcode / 100
	return majorVersion >= requiredXcodeMajor
}
