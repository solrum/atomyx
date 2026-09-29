package conformance

import (
	"context"
	"encoding/json"
	"fmt"
)

// Caller is the minimal surface RunCase needs from a driver: a
// blocking JSON-RPC request/response round trip. goSidecarCaller
// implements Caller by dispatching directly against the in-process Go
// sidecar, with no change to RunCase needed for a different Caller
// implementation.
type Caller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// StepResult is the outcome of replaying one Step against a Caller.
type StepResult struct {
	Method string
	Report string
	Equal  bool
}

// RunCase replays every step of c against caller in order and diffs
// each response against the step's recorded baseline. It stops and
// returns an error on the first call that fails outright (a transport
// error or a JSON-RPC error response); a call that succeeds but
// returns a mismatched value is recorded as a non-equal StepResult
// rather than an error, so a case with several steps reports every
// mismatch instead of only the first.
func RunCase(ctx context.Context, caller Caller, c *Case) ([]StepResult, error) {
	results := make([]StepResult, 0, len(c.Steps))
	for _, step := range c.Steps {
		var params any
		if len(step.Params) > 0 {
			params = step.Params
		}
		got, err := caller.Call(ctx, step.Method, params)
		if err != nil {
			return results, fmt.Errorf("calling %s: %w", step.Method, err)
		}
		report, equal, err := DiffJSON(step.Expect, got)
		if err != nil {
			return results, fmt.Errorf("diffing %s response: %w", step.Method, err)
		}
		results = append(results, StepResult{Method: step.Method, Report: report, Equal: equal})
	}
	return results, nil
}
