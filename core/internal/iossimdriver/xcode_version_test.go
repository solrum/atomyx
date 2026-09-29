package iossimdriver_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
)

func TestIsSimDirectSupported_NonArm64ReturnsFalseWithoutRunningAnything(t *testing.T) {
	if runtime.GOARCH == "arm64" {
		t.Skip("this host is arm64; the arch guard short-circuits before any Runner call only on non-arm64 hosts")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	if iossimdriver.IsSimDirectSupported(context.Background(), runner) {
		t.Fatal("IsSimDirectSupported() = true on a non-arm64 host, want false")
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want none on a non-arm64 host", runner.Invocations())
	}
}

func TestIsSimDirectSupported_VerifiedXcodeMajorReturnsTrue(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: "/Applications/Xcode.app/Contents/Developer\n"}, nil)
	runner.Stub(
		process.ExactMatch("plutil", "-extract", "DTXcode", "raw", "/Applications/Xcode.app/Contents/Info.plist"),
		process.Result{Stdout: "1620\n"},
		nil,
	)
	if !iossimdriver.IsSimDirectSupported(context.Background(), runner) {
		t.Fatal("IsSimDirectSupported() = false, want true for Xcode 16.2")
	}
}

func TestIsSimDirectSupported_BelowFloorReturnsFalse(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: "/Applications/Xcode.app/Contents/Developer\n"}, nil)
	runner.Stub(
		process.ExactMatch("plutil", "-extract", "DTXcode", "raw", "/Applications/Xcode.app/Contents/Info.plist"),
		process.Result{Stdout: "1510\n"},
		nil,
	)
	if iossimdriver.IsSimDirectSupported(context.Background(), runner) {
		t.Fatal("IsSimDirectSupported() = true, want false for Xcode 15.1")
	}
}

func TestIsSimDirectSupported_XcodeSelectFailureReturnsFalse(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	runner.DefaultErr = errors.New("xcode-select: error: unable to get active developer directory")
	if iossimdriver.IsSimDirectSupported(context.Background(), runner) {
		t.Fatal("IsSimDirectSupported() = true, want false when xcode-select fails")
	}
}

func TestIsSimDirectSupported_MalformedPlistValueReturnsFalse(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: "/Applications/Xcode.app/Contents/Developer\n"}, nil)
	runner.Stub(
		process.ExactMatch("plutil", "-extract", "DTXcode", "raw", "/Applications/Xcode.app/Contents/Info.plist"),
		process.Result{Stdout: "not-a-number\n"},
		nil,
	)
	if iossimdriver.IsSimDirectSupported(context.Background(), runner) {
		t.Fatal("IsSimDirectSupported() = true, want false for an unparsable DTXcode value")
	}
}

func TestIsSimDirectSupported_MemoizesAcrossCalls(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires an arm64 host to reach the Xcode version probe")
	}
	iossimdriver.ResetSimDirectCacheForTests()
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("xcode-select", "-p"), process.Result{Stdout: "/Applications/Xcode.app/Contents/Developer\n"}, nil)
	runner.Stub(
		process.ExactMatch("plutil", "-extract", "DTXcode", "raw", "/Applications/Xcode.app/Contents/Info.plist"),
		process.Result{Stdout: "1620\n"},
		nil,
	)
	ctx := context.Background()
	if !iossimdriver.IsSimDirectSupported(ctx, runner) {
		t.Fatal("first IsSimDirectSupported() = false, want true")
	}
	if len(runner.Invocations()) != 2 {
		t.Fatalf("invocations after first call = %+v, want exactly two", runner.Invocations())
	}
	if !iossimdriver.IsSimDirectSupported(ctx, runner) {
		t.Fatal("second IsSimDirectSupported() = false, want the memoized true")
	}
	if len(runner.Invocations()) != 2 {
		t.Fatalf("invocations after second call = %+v, want no new Runner calls", runner.Invocations())
	}
}
