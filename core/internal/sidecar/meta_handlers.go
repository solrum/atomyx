package sidecar

import (
	"context"
	"encoding/json"
)

// ProtocolVersion is the sidecar wire protocol version this build
// implements. A host calls protocolVersion to reject a sidecar build
// too old or too new for it.
const ProtocolVersion = 1

type protocolVersionResult struct {
	Version int `json:"version"`
}

type listMethodsResult struct {
	Methods []string `json:"methods"`
}

type pingResult struct {
	Ok bool `json:"ok"`
}

// RegisterMetaFeature wires the version and method-discovery
// handlers: protocolVersion, listMethods, ping.
func RegisterMetaFeature(ctx *Context) Handle {
	mustRegister(ctx.Dispatcher, "protocolVersion", func(context.Context, json.RawMessage) (any, error) {
		return protocolVersionResult{Version: ProtocolVersion}, nil
	})
	mustRegister(ctx.Dispatcher, "listMethods", func(context.Context, json.RawMessage) (any, error) {
		return listMethodsResult{Methods: ctx.Dispatcher.Methods()}, nil
	})
	mustRegister(ctx.Dispatcher, "ping", func(context.Context, json.RawMessage) (any, error) {
		return pingResult{Ok: true}, nil
	})
	return Handle{}
}
