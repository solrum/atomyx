package androiddriver

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/transport"
)

const (
	typeRejectedReason = "the text was not entered into the field"
	typeRejectedRemedy = "Focus the field and type again. Check the field content before continuing."

	gestureRejectedReason = "the gesture was not performed"
	gestureRejectedRemedy = "Make sure the screen is on and unlocked, then repeat the call."
)

// TypeRejectionError is returned by InputText when the agent reports
// that it did not enter the whole text. Typed and Total are the
// character counts the agent reported.
type TypeRejectionError struct {
	Reason string
	Typed  int
	Total  int
}

// AgentRejection reports the agent's own reason, or a fixed
// description when the agent gave none, and states how many characters
// were entered when the agent reported a partial entry.
func (e *TypeRejectionError) AgentRejection() (string, string) {
	reason := typeRejectedReason
	if strings.TrimSpace(e.Reason) != "" {
		reason = e.Reason
	}
	if e.Total > 0 && e.Typed < e.Total {
		reason += fmt.Sprintf(" (%d of %d characters entered)", e.Typed, e.Total)
	}
	return reason, typeRejectedRemedy
}

func (e *TypeRejectionError) Error() string {
	return fmt.Sprintf("androiddriver: type text rejected: typed=%d total=%d reason=%q", e.Typed, e.Total, e.Reason)
}

// typeRejection returns the error for a type reply that reports a
// failed or partial entry, or nil when the reply reports success. A
// reply that carries none of the fields is not a rejection, because an
// agent that omits them gives no verdict to act on.
func typeRejection(res transport.TypeKeyboardResult) error {
	if res == (transport.TypeKeyboardResult{}) {
		return nil
	}
	if res.Success && res.Typed >= res.Total {
		return nil
	}
	return &TypeRejectionError{Reason: res.Reason, Typed: res.Typed, Total: res.Total}
}

// GestureRejectionError is returned by a gesture method when the agent
// reports that the gesture was rejected, cancelled, or timed out.
type GestureRejectionError struct {
	Reason string
	Code   string
}

// AgentRejection reports the agent's own reason, or a fixed
// description when the agent gave none.
func (e *GestureRejectionError) AgentRejection() (string, string) {
	reason := gestureRejectedReason
	if strings.TrimSpace(e.Reason) != "" {
		reason = e.Reason
	}
	return reason, gestureRejectedRemedy
}

func (e *GestureRejectionError) Error() string {
	return fmt.Sprintf("androiddriver: gesture rejected: code=%q reason=%q", e.Code, e.Reason)
}

func gestureRejection(ok bool, reason, code *string) error {
	if ok {
		return nil
	}
	e := &GestureRejectionError{}
	if reason != nil {
		e.Reason = *reason
	}
	if code != nil {
		e.Code = *code
	}
	return e
}

var (
	_ agentRejection = (*TypeRejectionError)(nil)
	_ agentRejection = (*GestureRejectionError)(nil)
)
