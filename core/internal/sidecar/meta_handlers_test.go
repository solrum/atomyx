package sidecar

import (
	"context"
	"testing"
)

func newTestContext() *Context {
	return &Context{Dispatcher: NewDispatcher(), Events: NewEventBus(), Session: NewSession()}
}

func TestMeta_ProtocolVersion_ReturnsCurrentVersion(t *testing.T) {
	ctx := newTestContext()
	RegisterMetaFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "protocolVersion", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(protocolVersionResult)
	if !ok || got.Version != ProtocolVersion {
		t.Fatalf("Dispatch() result = %#v, want version %d", result, ProtocolVersion)
	}
}

func TestMeta_Ping_ReturnsOk(t *testing.T) {
	ctx := newTestContext()
	RegisterMetaFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got, ok := result.(pingResult); !ok || !got.Ok {
		t.Fatalf("Dispatch() result = %#v, want {Ok:true}", result)
	}
}

func TestMeta_ListMethods_ReturnsEveryRegisteredMethodSorted(t *testing.T) {
	ctx := newTestContext()
	RegisterMetaFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "listMethods", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(listMethodsResult)
	if !ok {
		t.Fatalf("Dispatch() result = %#v, want listMethodsResult", result)
	}
	want := []string{"listMethods", "ping", "protocolVersion"}
	if len(got.Methods) != len(want) {
		t.Fatalf("Methods = %v, want %v", got.Methods, want)
	}
	for i, m := range want {
		if got.Methods[i] != m {
			t.Fatalf("Methods = %v, want %v", got.Methods, want)
		}
	}
}
