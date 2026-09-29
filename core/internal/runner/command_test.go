package runner

import (
	"context"
	"testing"
)

// alwaysNoCapture is a NetworkCapture whose IsNoCaptureConfigured
// reports true, standing in for a composition root's default no-op
// adapter (a real NullCapture lives in package netcapture, which
// cannot be imported here without an import cycle).
type alwaysNoCapture struct{}

func (alwaysNoCapture) Start(context.Context) error { return nil }
func (alwaysNoCapture) Stop(context.Context) error  { return nil }
func (alwaysNoCapture) WaitForRequest(context.Context, string, int) (CapturedRequest, error) {
	return CapturedRequest{}, nil
}
func (alwaysNoCapture) All() []CapturedRequest      { return nil }
func (alwaysNoCapture) Reset()                      {}
func (alwaysNoCapture) IsNoCaptureConfigured() bool { return true }

func TestNoCaptureConfiguredNil(t *testing.T) {
	if !noCaptureConfigured(nil) {
		t.Fatal("noCaptureConfigured(nil): got false, want true")
	}
}

func TestNoCaptureConfiguredDefaultAdapter(t *testing.T) {
	if !noCaptureConfigured(alwaysNoCapture{}) {
		t.Fatal("noCaptureConfigured(alwaysNoCapture{}): got false, want true")
	}
}

func TestNoCaptureConfiguredRealAdapter(t *testing.T) {
	if noCaptureConfigured(&fakeNetworkCapture{}) {
		t.Fatal("noCaptureConfigured(&fakeNetworkCapture{}): got true, want false")
	}
}
