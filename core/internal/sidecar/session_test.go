package sidecar

import (
	"errors"
	"testing"
)

func TestSession_Device_NoneSelected_ReturnsNil(t *testing.T) {
	s := NewSession()
	if got := s.Device(); got != nil {
		t.Fatalf("Device() = %+v, want nil", got)
	}
}

func TestSession_RequireDevice_NoneSelected_ReturnsErrSessionNoDevice(t *testing.T) {
	s := NewSession()
	_, err := s.RequireDevice()
	if !errors.Is(err, ErrSessionNoDevice) {
		t.Fatalf("RequireDevice() error = %v, want ErrSessionNoDevice", err)
	}
}

func TestSession_SetDevice_FirstSelection_DoesNotDispose(t *testing.T) {
	s := NewSession()
	disposed := false
	sel := &DeviceSelection{ID: "d1", Dispose: func() error { disposed = true; return nil }}

	s.SetDevice(sel)

	if s.Device() != sel {
		t.Fatalf("Device() = %+v, want %+v", s.Device(), sel)
	}
	if disposed {
		t.Fatal("Dispose() was called on the first selection")
	}
}

func TestSession_SetDevice_SameID_IsNoOpAndDoesNotDispose(t *testing.T) {
	s := NewSession()
	disposed := false
	sel := &DeviceSelection{ID: "d1", Dispose: func() error { disposed = true; return nil }}
	s.SetDevice(sel)

	same := &DeviceSelection{ID: "d1"}
	s.SetDevice(same)

	if s.Device() != sel {
		t.Fatalf("Device() = %+v, want the original selection kept", s.Device())
	}
	if disposed {
		t.Fatal("Dispose() was called when re-selecting the same device id")
	}
}

func TestSession_SetDevice_SameID_DisposesTheDiscardedIncomingSelection(t *testing.T) {
	s := NewSession()
	kept := &DeviceSelection{ID: "d1"}
	s.SetDevice(kept)

	incomingDisposed := false
	redundant := &DeviceSelection{ID: "d1", Dispose: func() error { incomingDisposed = true; return nil }}
	s.SetDevice(redundant)

	if s.Device() != kept {
		t.Fatalf("Device() = %+v, want the original selection kept", s.Device())
	}
	if !incomingDisposed {
		t.Fatal("Dispose() was not called on the discarded incoming selection, which leaks its live driver connection")
	}
}

func TestSession_SetDevice_DifferentID_DisposesPrevious(t *testing.T) {
	s := NewSession()
	disposed := false
	first := &DeviceSelection{ID: "d1", Dispose: func() error { disposed = true; return nil }}
	s.SetDevice(first)

	second := &DeviceSelection{ID: "d2"}
	s.SetDevice(second)

	if s.Device() != second {
		t.Fatalf("Device() = %+v, want %+v", s.Device(), second)
	}
	if !disposed {
		t.Fatal("Dispose() was not called on the replaced device")
	}
}

func TestSession_SetDevice_Nil_DeselectsAndDisposesPrevious(t *testing.T) {
	s := NewSession()
	disposed := false
	first := &DeviceSelection{ID: "d1", Dispose: func() error { disposed = true; return nil }}
	s.SetDevice(first)

	s.SetDevice(nil)

	if s.Device() != nil {
		t.Fatalf("Device() = %+v, want nil", s.Device())
	}
	if !disposed {
		t.Fatal("Dispose() was not called after deselecting")
	}
}

func TestSession_SetDevice_DisposeError_IsSwallowed(t *testing.T) {
	s := NewSession()
	first := &DeviceSelection{ID: "d1", Dispose: func() error { return errors.New("boom") }}
	s.SetDevice(first)

	// Must not panic and must still install the new selection despite
	// the previous device's Dispose returning an error.
	second := &DeviceSelection{ID: "d2"}
	s.SetDevice(second)

	if s.Device() != second {
		t.Fatalf("Device() = %+v, want %+v", s.Device(), second)
	}
}

func TestSession_ActiveRunID_RoundTrips(t *testing.T) {
	s := NewSession()
	if got := s.ActiveRunID(); got != nil {
		t.Fatalf("ActiveRunID() = %v, want nil", got)
	}

	runID := "run-1"
	s.SetActiveRunID(&runID)
	if got := s.ActiveRunID(); got == nil || *got != runID {
		t.Fatalf("ActiveRunID() = %v, want %q", got, runID)
	}

	s.SetActiveRunID(nil)
	if got := s.ActiveRunID(); got != nil {
		t.Fatalf("ActiveRunID() = %v, want nil after clearing", got)
	}
}

func TestSession_Dispose_ClearsDeviceAndRunAndDisposesDevice(t *testing.T) {
	s := NewSession()
	disposed := false
	sel := &DeviceSelection{ID: "d1", Dispose: func() error { disposed = true; return nil }}
	s.SetDevice(sel)
	runID := "run-1"
	s.SetActiveRunID(&runID)

	s.Dispose()

	if s.Device() != nil {
		t.Fatalf("Device() = %+v, want nil after Dispose", s.Device())
	}
	if s.ActiveRunID() != nil {
		t.Fatalf("ActiveRunID() = %v, want nil after Dispose", s.ActiveRunID())
	}
	if !disposed {
		t.Fatal("Dispose() did not release the selected device")
	}
}
