package sidecar

import (
	"encoding/json"
	"errors"
)

// errNoDeviceSelected is the wire-facing error every feature handler
// reports when a call requires a selected device but none is
// selected. Its text is part of the wire contract callers match on,
// so it is kept distinct from Session.RequireDevice's own
// ErrSessionNoDevice, whose message is Go-package-scoped.
var errNoDeviceSelected = errors.New("No device selected") //nolint:staticcheck // this exact string crosses the wire protocol and is matched verbatim by other sidecar implementations, so its casing is part of the contract, not a style choice

// requireDeviceOrWireError adapts Session.RequireDevice for a
// handler: any failure becomes errNoDeviceSelected, the exact message
// a caller sees on the wire.
func requireDeviceOrWireError(session *Session) (*DeviceSelection, error) {
	selection, err := session.RequireDevice()
	if err != nil {
		return nil, errNoDeviceSelected
	}
	return selection, nil
}

// decodeParams decodes a request's params into a string-keyed map.
// Params that are absent, not a JSON object, or fail to decode all
// resolve to an empty map rather than an error: a handler's own
// field-presence check is the sole source of an InvalidParams
// response, matching the ported handlers' own permissive field access
// on an untyped params value.
func decodeParams(params json.RawMessage) map[string]any {
	if len(params) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(params, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// mustRegister registers method on d, panicking if it is already
// registered. Registration happens once, at process startup, so a
// collision is a wiring bug the composition root must fail on
// immediately rather than silently drop a handler.
func mustRegister(d *Dispatcher, method string, handler Handler) {
	if err := d.Register(method, handler); err != nil {
		panic(err)
	}
}
