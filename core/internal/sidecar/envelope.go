package sidecar

import (
	"encoding/json"
	"errors"
)

// ErrorCode is the closed set of wire error codes a Response may
// carry. The set is closed deliberately: a client can switch on the
// exact string values below without a default case, and adding a new
// code is a wire-contract change that must be made here, not
// improvised by a handler.
type ErrorCode string

const (
	ErrParseError       ErrorCode = "ParseError"
	ErrInvalidRequest   ErrorCode = "InvalidRequest"
	ErrMethodNotFound   ErrorCode = "MethodNotFound"
	ErrInvalidParams    ErrorCode = "InvalidParams"
	ErrInternalError    ErrorCode = "InternalError"
	ErrNotConnected     ErrorCode = "NotConnected"
	ErrNoDeviceSelected ErrorCode = "NoDeviceSelected"
	ErrDeviceBusy       ErrorCode = "DeviceBusy"
	ErrInvalidScript    ErrorCode = "InvalidScript"
	ErrSelectorNotFound ErrorCode = "SelectorNotFound"
	ErrTimeout          ErrorCode = "Timeout"
	ErrPlatformError    ErrorCode = "PlatformError"
)

// Request is one line of client-to-sidecar JSON. Params is left
// undecoded because a handler owns its own params validation and
// decodes into whatever shape it expects.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// WireError is the error object carried by an error Response.
type WireError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
}

// Response is one line of sidecar-to-client JSON answering a
// Request. Exactly one of a successful Result or an Error is
// present on the wire: a nil Error marshals a success response
// carrying Result (which may itself be JSON null); a non-nil Error
// marshals an error response and Result is not emitted at all.
type Response struct {
	ID     string
	Result any
	Error  *WireError
}

// successWire and errorWire are the two shapes Response marshals to.
// Keeping them as separate structs (rather than putting omitempty on
// Response's own fields) guarantees "result" is always present on a
// success response, including when Result is nil.
type successWire struct {
	ID     string `json:"id"`
	Result any    `json:"result"`
}

type errorWire struct {
	ID    string    `json:"id"`
	Error WireError `json:"error"`
}

// MarshalJSON implements json.Marshaler.
func (r Response) MarshalJSON() ([]byte, error) {
	if r.Error != nil {
		return json.Marshal(errorWire{ID: r.ID, Error: *r.Error})
	}
	return json.Marshal(successWire{ID: r.ID, Result: r.Result})
}

// SuccessResponse builds a success Response carrying result. A nil
// result marshals as a "result" key holding JSON null, matching a
// handler that returns no value.
func SuccessResponse(id string, result any) Response {
	return Response{ID: id, Result: result}
}

// ErrorResponse builds an error Response. data is omitted from the
// wire when nil.
func ErrorResponse(id string, code ErrorCode, message string, data any) Response {
	return Response{ID: id, Error: &WireError{Code: code, Message: message, Data: data}}
}

// Event is a server-to-client notification pushed outside the
// request/response cycle: progress, device-state changes, script
// step updates. It carries no id because it is not a reply to any
// specific request.
type Event struct {
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

// errMalformedJSON and errMissingFields distinguish the two ways a
// raw input line can fail to become a Request, so the caller can
// pick the matching wire error code (ParseError vs InvalidRequest).
var (
	errMalformedJSON = errors.New("sidecar: malformed JSON")
	errMissingFields = errors.New("sidecar: message missing required fields (id, method)")
)

// ParseRequest decodes one line of input into a Request. It returns
// errMalformedJSON when line is not valid JSON, and errMissingFields
// when line parses but lacks a string "id" or a string "method", so
// the caller can distinguish a JSON-RPC ParseError from an
// InvalidRequest and reply with the matching wire error code.
func ParseRequest(line []byte) (Request, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return Request{}, errMalformedJSON
	}

	idRaw, hasID := raw["id"]
	methodRaw, hasMethod := raw["method"]
	if !hasID || !hasMethod {
		return Request{}, errMissingFields
	}

	var id, method string
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return Request{}, errMissingFields
	}
	if err := json.Unmarshal(methodRaw, &method); err != nil {
		return Request{}, errMissingFields
	}

	req := Request{ID: id, Method: method}
	if params, ok := raw["params"]; ok {
		req.Params = params
	}
	return req, nil
}
