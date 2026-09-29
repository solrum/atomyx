package androiddriver

import "fmt"

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
	Diagnostic ClearTextDiagnostic
}

func (e *ClearTextError) Error() string {
	return fmt.Sprintf(
		"androiddriver: clear text failed after all strategies: tried=%v lastValue=%q",
		e.Diagnostic.StrategiesTried, e.Diagnostic.LastValue,
	)
}
