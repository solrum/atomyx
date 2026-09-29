package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestDispatcher_Dispatch_RegisteredMethod_ReturnsHandlerResult(t *testing.T) {
	d := NewDispatcher()
	if err := d.Register("echo", func(_ context.Context, params json.RawMessage) (any, error) {
		return string(params), nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := d.Dispatch(context.Background(), "echo", json.RawMessage(`"hi"`))
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result != `"hi"` {
		t.Fatalf("Dispatch() result = %v, want %q", result, `"hi"`)
	}
}

func TestDispatcher_Dispatch_UnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	d := NewDispatcher()
	_, err := d.Dispatch(context.Background(), "missing", nil)

	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("Dispatch() error = %v, want a *DispatcherError", err)
	}
	if dispatchErr.Code != ErrMethodNotFound {
		t.Fatalf("Dispatch() error code = %v, want %v", dispatchErr.Code, ErrMethodNotFound)
	}
}

func TestDispatcher_Dispatch_HandlerReturnsDispatcherError_PassesThrough(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("select", func(context.Context, json.RawMessage) (any, error) {
		return nil, NewDispatcherError(ErrNoDeviceSelected, "no device", map[string]any{"hint": "call selectDevice first"})
	})

	_, err := d.Dispatch(context.Background(), "select", nil)
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("Dispatch() error = %v, want a *DispatcherError", err)
	}
	if dispatchErr.Code != ErrNoDeviceSelected || dispatchErr.Message != "no device" {
		t.Fatalf("Dispatch() error = %+v, want code=%v message=%q", dispatchErr, ErrNoDeviceSelected, "no device")
	}
}

func TestDispatcher_Dispatch_HandlerPanics_ReturnsError(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("boom", func(context.Context, json.RawMessage) (any, error) {
		panic("kaboom")
	})

	_, err := d.Dispatch(context.Background(), "boom", nil)
	if err == nil {
		t.Fatal("Dispatch() error = nil, want a non-nil error after a handler panic")
	}
}

func TestDispatcher_Register_DuplicateMethod_ReturnsError(t *testing.T) {
	d := NewDispatcher()
	noop := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	if err := d.Register("dup", noop); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := d.Register("dup", noop); err == nil {
		t.Fatal("second Register() error = nil, want an error for a duplicate method")
	}
}

func TestDispatcher_HandleRequest_Success_BuildsSuccessResponse(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	})

	got := d.HandleRequest(context.Background(), Request{ID: "1", Method: "ping"})
	want := SuccessResponse("1", "pong")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HandleRequest() = %+v, want %+v", got, want)
	}
}

func TestDispatcher_HandleRequest_DispatcherError_BuildsMatchingErrorResponse(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("fail", func(context.Context, json.RawMessage) (any, error) {
		return nil, NewDispatcherError(ErrTimeout, "waited too long", nil)
	})

	got := d.HandleRequest(context.Background(), Request{ID: "2", Method: "fail"})
	want := ErrorResponse("2", ErrTimeout, "waited too long", nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HandleRequest() = %+v, want %+v", got, want)
	}
}

func TestDispatcher_HandleRequest_PlainError_BecomesInternalError(t *testing.T) {
	d := NewDispatcher()
	_ = d.Register("crash", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("disk on fire")
	})

	got := d.HandleRequest(context.Background(), Request{ID: "3", Method: "crash"})
	if got.Error == nil || got.Error.Code != ErrInternalError || got.Error.Message != "disk on fire" {
		t.Fatalf("HandleRequest() = %+v, want an InternalError with message %q", got, "disk on fire")
	}
}

func TestDispatcher_HandleRequest_UnknownMethod_BecomesMethodNotFoundResponse(t *testing.T) {
	d := NewDispatcher()
	got := d.HandleRequest(context.Background(), Request{ID: "4", Method: "nope"})
	if got.Error == nil || got.Error.Code != ErrMethodNotFound {
		t.Fatalf("HandleRequest() = %+v, want MethodNotFound", got)
	}
}

func TestDispatcher_Methods_ReturnsSortedRegisteredMethods(t *testing.T) {
	d := NewDispatcher()
	noop := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	_ = d.Register("zebra", noop)
	_ = d.Register("alpha", noop)
	_ = d.Register("mango", noop)

	got := d.Methods()
	want := []string{"alpha", "mango", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Methods() = %v, want %v", got, want)
	}
}
