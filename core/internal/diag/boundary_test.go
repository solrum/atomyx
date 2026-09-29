package diag

import (
	"context"
	"errors"
	"testing"
)

// alwaysUnreachable, neverInvalidArgument, and neverRejected stand in
// for an adapter's own recognizer functions in tests that only care
// about a single branch of ClassifyAgentBoundary's classification.
func alwaysUnreachable(error) bool              { return true }
func neverUnreachable(error) bool               { return false }
func neverInvalidArgument(error) (string, bool) { return "", false }
func neverRejected(error) (string, bool)        { return "", false }
func invalidArgumentWith(message string) func(error) (string, bool) {
	return func(error) (string, bool) { return message, true }
}
func rejectedWith(message string) func(error) (string, bool) {
	return func(error) (string, bool) { return message, true }
}

func TestClassifyAgentBoundary_NilStaysNil(t *testing.T) {
	if got := ClassifyAgentBoundary(nil, "device-1", neverUnreachable, neverInvalidArgument, neverRejected); got != nil {
		t.Fatalf("ClassifyAgentBoundary(nil) = %v, want nil", got)
	}
}

func TestClassifyAgentBoundary_ContextErrorStaysAsIs(t *testing.T) {
	got := ClassifyAgentBoundary(context.Canceled, "device-1", alwaysUnreachable, neverInvalidArgument, rejectedWith("x"))
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("ClassifyAgentBoundary(context.Canceled) = %v, want it to stay context.Canceled", got)
	}
}

func TestClassifyAgentBoundary_AlreadyCodedErrorStaysAsIs(t *testing.T) {
	original := New(CodeToolTimeout, "already classified", "", nil)
	got := ClassifyAgentBoundary(original, "device-1", alwaysUnreachable, neverInvalidArgument, rejectedWith("x"))
	if !errors.Is(got, original) {
		t.Fatalf("ClassifyAgentBoundary(already-coded) = %v, want the original error unchanged", got)
	}
}

func TestClassifyAgentBoundary_Unreachable(t *testing.T) {
	cause := errors.New("dial failed")
	got := ClassifyAgentBoundary(cause, "device-1", alwaysUnreachable, neverInvalidArgument, neverRejected)
	code, message, remedy := Describe(got)
	if code != CodeAgentUnreachable {
		t.Fatalf("code = %q, want %q", code, CodeAgentUnreachable)
	}
	if message != "could not reach the agent on device device-1" {
		t.Fatalf("message = %q, want it to name the device", message)
	}
	if remedy != AgentUnreachableRemedy {
		t.Fatalf("remedy = %q, want %q", remedy, AgentUnreachableRemedy)
	}
	if !errors.Is(got, cause) && !errors.Is(errors.Unwrap(got), cause) {
		t.Fatalf("ClassifyAgentBoundary(unreachable) does not preserve cause as Unwrap")
	}
}

func TestClassifyAgentBoundary_InvalidArgumentSanitizesTheAgentMessage(t *testing.T) {
	cause := errors.New("agent said no")
	got := ClassifyAgentBoundary(cause, "device-1", neverUnreachable, invalidArgumentWith("GET /tree failed"), neverRejected)
	code, message, remedy := Describe(got)
	if code != CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", code, CodeInvalidArgument)
	}
	if message != neutralAgentRejectedMessage {
		t.Fatalf("message = %q, want the sanitized neutral message since the raw message names a route", message)
	}
	if remedy != InvalidArgumentRemedy {
		t.Fatalf("remedy = %q, want %q", remedy, InvalidArgumentRemedy)
	}
}

func TestClassifyAgentBoundary_InvalidArgumentKeepsACleanAgentMessage(t *testing.T) {
	got := ClassifyAgentBoundary(errors.New("agent said no"), "device-1", neverUnreachable,
		invalidArgumentWith("Unsupported key: x. Supported keys: back, home."), neverRejected)
	_, message, _ := Describe(got)
	if message != "Unsupported key: x. Supported keys: back, home." {
		t.Fatalf("message = %q, want the clean agent message preserved", message)
	}
}

func TestClassifyAgentBoundary_InvalidArgumentTakesPriorityOverRejected(t *testing.T) {
	got := ClassifyAgentBoundary(errors.New("agent said no"), "device-1", neverUnreachable,
		invalidArgumentWith("bad argument"), rejectedWith("generic rejection"))
	code, _, _ := Describe(got)
	if code != CodeInvalidArgument {
		t.Fatalf("code = %q, want %q when both invalidArgument and rejected match", code, CodeInvalidArgument)
	}
}

func TestClassifyAgentBoundary_RejectedSanitizesTheAgentMessage(t *testing.T) {
	cause := errors.New("agent said no")
	got := ClassifyAgentBoundary(cause, "device-1", neverUnreachable, neverInvalidArgument, rejectedWith("GET /tree failed"))
	code, message, remedy := Describe(got)
	if code != CodeAgentRejected {
		t.Fatalf("code = %q, want %q", code, CodeAgentRejected)
	}
	if message != neutralAgentRejectedMessage {
		t.Fatalf("message = %q, want the sanitized neutral message since the raw message names a route", message)
	}
	if remedy != AgentRejectedRemedy {
		t.Fatalf("remedy = %q, want %q", remedy, AgentRejectedRemedy)
	}
}

func TestClassifyAgentBoundary_RejectedKeepsACleanAgentMessage(t *testing.T) {
	got := ClassifyAgentBoundary(errors.New("agent said no"), "device-1", neverUnreachable, neverInvalidArgument, rejectedWith("Element not found"))
	_, message, _ := Describe(got)
	if message != "Element not found" {
		t.Fatalf("message = %q, want the clean agent message preserved", message)
	}
}

func TestClassifyAgentBoundary_UnclassifiedFallsBackToCommunicationFailed(t *testing.T) {
	got := ClassifyAgentBoundary(errors.New("something else"), "device-1", neverUnreachable, neverInvalidArgument, neverRejected)
	code, message, remedy := Describe(got)
	if code != CodeAgentCommunicationFailed {
		t.Fatalf("code = %q, want %q", code, CodeAgentCommunicationFailed)
	}
	if message != "could not complete the request to the agent on device device-1" {
		t.Fatalf("message = %q, want it to name the device", message)
	}
	if remedy != AgentCommunicationFailedRemedy {
		t.Fatalf("remedy = %q, want %q", remedy, AgentCommunicationFailedRemedy)
	}
}
