package androiddriver

import (
	"fmt"
	"regexp"
	"strings"
)

// ClearTextDiagnostic carries the Android agent's own diagnosis for
// why the focused field's value would not clear: every strategy it
// tried, the value that remained afterward, a description of the
// focused node, and the screen dimensions at the time.
type ClearTextDiagnostic struct {
	StrategiesTried []string
	LastValue       string
	FocusedNodeDesc string
	ScreenWidth     int
	ScreenHeight    int
}

// ClearTextError is returned by EraseText when the Android agent
// exhausts every clear-text strategy without emptying the focused
// field. Diagnostic carries enough state for a caller to report why,
// rather than just that the clear failed.
type ClearTextError struct {
	// Reason is the agent's own one-line explanation, possibly empty.
	Reason     string
	Diagnostic ClearTextDiagnostic
}

const (
	clearTextFailedReason = "the focused input could not be cleared"
	clearTextFailedRemedy = "Focus an input field that holds text, then call again."
)

// focusLost reports whether the agent failed because no input node held
// focus when it tried to clear, as opposed to a field that held focus
// but resisted every strategy. Every focus-related agent reason names
// the focus.
func (e *ClearTextError) focusLost() bool {
	return strings.Contains(strings.ToLower(e.Reason), "focus")
}

// AgentRejection reports the agent's own reason, or a fixed
// description when the agent gave none. When the agent tried clear
// strategies, their names are appended so the caller sees what was
// attempted.
func (e *ClearTextError) AgentRejection() (string, string) {
	reason := clearTextFailedReason
	if strings.TrimSpace(e.Reason) != "" {
		reason = e.Reason
	}
	if tried := safeStrategyNames(e.Diagnostic.StrategiesTried); len(tried) > 0 {
		reason += " (strategies tried: " + strings.Join(tried, ", ") + ")"
	}
	return reason, clearTextFailedRemedy
}

const maxReportedStrategies = 6

var strategyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)

// safeStrategyNames keeps the leading strategy names that are plain
// identifiers, capped at maxReportedStrategies, so an unexpected agent
// string never reaches the caller-facing message.
func safeStrategyNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if len(out) == maxReportedStrategies {
			break
		}
		if strategyNamePattern.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

var _ agentRejection = (*ClearTextError)(nil)

func (e *ClearTextError) Error() string {
	return fmt.Sprintf(
		"androiddriver: clear text failed after all strategies: tried=%v lastValue=%q",
		e.Diagnostic.StrategiesTried, e.Diagnostic.LastValue,
	)
}
