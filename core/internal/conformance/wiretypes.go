package conformance

import "encoding/json"

// RPCError is a JSON-RPC {code, message, data} error response,
// returned by a Caller's Call method whenever a request resolves with
// an error rather than a result.
type RPCError struct {
	Code    string
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	return e.Code + ": " + e.Message
}

// Event is one out-of-band {event, payload} message a sidecar
// dispatcher publishes independently of any request/response pair.
type Event struct {
	Name    string
	Payload json.RawMessage
}
