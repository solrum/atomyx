package report

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/scenario"
)

// SuiteAttemptScope builds the scope of one attempt of a suite entry from
// the entry's own scope. The first attempt keeps the entry scope, so a
// suite without retries scopes every screenshot as before, and a later
// attempt gets a scope of its own so its files never collide with an
// earlier attempt's. Like ScenarioChildScope, it is the single source of
// truth for the format.
func SuiteAttemptScope(entryScope string, attempt int) string {
	if attempt <= 1 {
		return entryScope
	}
	return fmt.Sprintf("%s::attempt-%d", entryScope, attempt)
}

// finalAttemptNumber is the attempt whose outcome an entry reports. An
// entry result built without attempts counts as one attempt.
func finalAttemptNumber(entry scenario.SuiteEntryResult) int {
	return max(len(entry.Attempts), 1)
}

// entryBadges renders the flaky and quarantined badges of a suite entry,
// each led by a space so they follow the entry heading text directly. It
// reuses the existing status styles, so the stylesheet stays unchanged.
func entryBadges(entry scenario.SuiteEntryResult) string {
	var out strings.Builder
	if entry.Flaky {
		fmt.Fprintf(&out, " <span class=\"status-errored\">flaky (%d attempts)</span>", len(entry.Attempts))
	}
	if entry.QuarantinedFailure() {
		out.WriteString(" <span class=\"status-skipped\">quarantined</span>")
	}
	return out.String()
}

// renderAttemptList renders one line per attempt of an entry that ran
// more than once, or nothing for an entry that ran once.
func renderAttemptList(entry scenario.SuiteEntryResult) string {
	if len(entry.Attempts) < 2 {
		return ""
	}
	items := make([]string, len(entry.Attempts))
	for i, attempt := range entry.Attempts {
		statusClass := "status-fail"
		if attempt.Status == scenario.EntryPassed {
			statusClass = "status-pass"
		}
		items[i] = fmt.Sprintf("<li>Attempt %d: <span class=\"%s\">%s</span>, %d ms</li>",
			attempt.Number, statusClass, escapeHTML(string(attempt.Status)), attempt.DurationMs)
	}
	return "\n  <ol class=\"attempts\">" + strings.Join(items, "") + "</ol>"
}

// renderQuarantinedSection lists the entries that failed while
// quarantined, or renders nothing when there are none.
func renderQuarantinedSection(result scenario.SuiteResult) string {
	var items []string
	for _, entry := range result.Entries {
		if entry.QuarantinedFailure() {
			items = append(items, "<li>"+escapeHTML(entry.Path)+"</li>")
		}
	}
	if len(items) == 0 {
		return ""
	}
	return "\n  <section class=\"quarantined\">\n    <h3>Quarantined failures</h3>\n    <ul>" + strings.Join(items, "") + "</ul>\n  </section>"
}

// suiteCountsNote renders the flaky and quarantined counts that follow
// the totals line, or an empty string when both are zero.
func suiteCountsNote(result scenario.SuiteResult) string {
	var parts []string
	if result.Flaky > 0 {
		parts = append(parts, fmt.Sprintf("%d flaky", result.Flaky))
	}
	if result.QuarantinedFailures > 0 {
		parts = append(parts, fmt.Sprintf("%d quarantined", result.QuarantinedFailures))
	}
	if len(parts) == 0 {
		return ""
	}
	return ", " + strings.Join(parts, ", ")
}

// ReportJsonAttempt is one attempt of a suite entry that ran more than
// once.
type ReportJsonAttempt struct {
	Number     int                       `json:"number"`
	Status     scenario.SuiteEntryStatus `json:"status"`
	Error      *string                   `json:"error,omitempty"`
	DurationMs int64                     `json:"durationMs"`
	// Videos are the kept video files of the attempt, relative to the
	// artifacts directory. Omitted when none were kept.
	Videos []string `json:"videos,omitempty"`
}

// jsonAttempts renders the attempts of an entry that ran more than once,
// or nil for an entry that ran once.
func jsonAttempts(entry scenario.SuiteEntryResult) []ReportJsonAttempt {
	if len(entry.Attempts) < 2 {
		return nil
	}
	out := make([]ReportJsonAttempt, len(entry.Attempts))
	for i, attempt := range entry.Attempts {
		out[i] = ReportJsonAttempt{
			Number:     attempt.Number,
			Status:     attempt.Status,
			Error:      attempt.Error,
			DurationMs: attempt.DurationMs,
			Videos:     attempt.Videos,
		}
	}
	return out
}
