package process

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFake_Run_MatchingStub_ReturnsCannedResponse(t *testing.T) {
	f := NewFake()
	f.Stub(ExactMatch("adb", "-s", "S1", "install", "-r", "app.apk"),
		Result{Stdout: "Success\n"}, nil)

	got, err := f.Run(context.Background(), "adb", "-s", "S1", "install", "-r", "app.apk")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Stdout != "Success\n" {
		t.Fatalf("Run() = %+v, want Stdout %q", got, "Success\n")
	}
}

func TestFake_Run_NoMatchingStub_ReturnsDefault(t *testing.T) {
	f := NewFake()
	f.Default = Result{Stdout: "fallback"}
	f.DefaultErr = errors.New("no stub configured")

	got, err := f.Run(context.Background(), "adb", "devices")
	if got.Stdout != "fallback" {
		t.Fatalf("Run() Stdout = %q, want %q", got.Stdout, "fallback")
	}
	if err == nil || err.Error() != "no stub configured" {
		t.Fatalf("Run() error = %v, want %q", err, "no stub configured")
	}
}

func TestFake_Run_FirstMatchingStubWins(t *testing.T) {
	f := NewFake()
	f.Stub(func(string, []string) bool { return true }, Result{Stdout: "first"}, nil)
	f.Stub(func(string, []string) bool { return true }, Result{Stdout: "second"}, nil)

	got, _ := f.Run(context.Background(), "adb", "devices")
	if got.Stdout != "first" {
		t.Fatalf("Run() Stdout = %q, want %q", got.Stdout, "first")
	}
}

func TestFake_Run_RecordsInvocationsInDispatchOrder(t *testing.T) {
	f := NewFake()
	_, _ = f.Run(context.Background(), "adb", "-s", "S1", "forward", "tcp:8765", "tcp:8765")
	_, _ = f.Run(context.Background(), "adb", "-s", "S1", "uninstall", "dev.atomyx.app")

	want := []Invocation{
		{Name: "adb", Args: []string{"-s", "S1", "forward", "tcp:8765", "tcp:8765"}},
		{Name: "adb", Args: []string{"-s", "S1", "uninstall", "dev.atomyx.app"}},
	}
	if got := f.Invocations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Invocations() = %+v, want %+v", got, want)
	}
}

func TestFake_Run_AlreadyCanceledContext_ReturnsContextErrorAndDoesNotRecord(t *testing.T) {
	f := NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.Run(ctx, "adb", "devices")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got := f.Invocations(); len(got) != 0 {
		t.Fatalf("Invocations() = %+v, want none recorded for a canceled call", got)
	}
}
