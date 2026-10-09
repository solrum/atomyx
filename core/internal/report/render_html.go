package report

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

// RenderHTML renders one self-contained HTML report document for
// input: a summary header, then a section per step, listing every
// step's outcome. The document inlines its own styling and
// references screenshot files through relative links, so it opens
// correctly as a plain file with no network access and no script
// framework.
//
// byScope is the run's screenshot index grouped by owning scope, as
// documented on RenderJSON. imageBase is the link prefix, ending in a
// slash, that leads from the report file to the directory holding the
// screenshots. ArtifactsLink computes it.
func RenderHTML(input ReportInput, byScope ScreenshotsByScope, imageBase string) (string, error) {
	var summary htmlSummary
	var body string

	switch v := input.(type) {
	case ScriptReportInput:
		summary = summarizeScript(v.Result)
		body = renderScriptSection(v.Result, RootScriptScope, byScope, imageBase)
	case ScenarioReportInput:
		summary = summarizeScenario(v.Result)
		body = renderScenarioSection(v.Result, byScope, imageBase)
	case SuiteReportInput:
		summary = summarizeSuite(v.Result)
		body = renderSuiteSection(v.Result, v.EntryVideos, byScope, imageBase)
	default:
		return "", fmt.Errorf("report: renderHtml does not support report kind %q yet", input.ReportKind())
	}

	metadata := input.ReportMetadata()
	header := renderHeader(metadata, summary, imageBase)

	return fmt.Sprintf(
		"<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Atomyx report: %s</title>\n<style>%s</style>\n</head>\n<body>\n%s\n%s\n</body>\n</html>\n",
		escapeHTML(metadata.Label), style, header, body,
	), nil
}

type htmlSummary struct {
	ok     bool
	totals string
}

func summarizeScript(result runner.ScriptResult) htmlSummary {
	return htmlSummary{
		ok:     result.Ok,
		totals: fmt.Sprintf("%d of %d steps passed", result.PassedSteps, result.TotalSteps),
	}
}

func summarizeScenario(result scenario.ScenarioResult) htmlSummary {
	return htmlSummary{
		ok:     result.Ok,
		totals: fmt.Sprintf("%d of %d scripts passed", result.PassedScripts, result.TotalScripts),
	}
}

func summarizeSuite(result scenario.SuiteResult) htmlSummary {
	return htmlSummary{
		ok:     result.Ok,
		totals: fmt.Sprintf("%d of %d flows passed, %d failed%s", result.Passed, result.Total, result.Failed, suiteCountsNote(result)),
	}
}

func renderHeader(metadata ReportMetadata, summary htmlSummary, imageBase string) string {
	status, statusClass := "FAIL", "status-fail"
	if summary.ok {
		status, statusClass = "PASS", "status-pass"
	}

	rows := []string{
		metaRow("Run id", metadata.RunID),
		metaRow("Command", string(metadata.Command)),
	}
	if metadata.Env != nil && *metadata.Env != "" {
		rows = append(rows, metaRow("Environment", *metadata.Env))
	}
	if metadata.Platform != nil && *metadata.Platform != "" {
		rows = append(rows, metaRow("Platform", *metadata.Platform))
	}
	if metadata.Device != nil && *metadata.Device != "" {
		rows = append(rows, metaRow("Device", *metadata.Device))
	}
	rows = append(rows,
		metaRow("Started", metadata.StartedAt),
		metaRow("Finished", metadata.FinishedAt),
		metaRow("Duration", fmt.Sprintf("%d ms", metadata.DurationMs)),
		metaRow("Totals", summary.totals),
	)
	if metadata.Abort != nil {
		rows = append(rows, metaRow("Aborted", fmt.Sprintf("%s (%s)", metadata.Abort.Message, metadata.Abort.Code)))
	}
	if metadata.Recording != nil {
		rows = append(rows, recordingRow(*metadata.Recording, imageBase))
	}

	return fmt.Sprintf(
		"<header class=\"report-header\">\n  <h1><span class=\"%s\">%s</span> %s</h1>\n  <dl class=\"meta\">\n    %s\n  </dl>\n</header>",
		statusClass, status, escapeHTML(metadata.Label), strings.Join(rows, "\n    "),
	)
}

func metaRow(term, value string) string {
	return fmt.Sprintf("<div><dt>%s</dt><dd>%s</dd></div>", escapeHTML(term), escapeHTML(value))
}

func renderScriptSection(result runner.ScriptResult, scope string, byScope ScreenshotsByScope, imageBase string) string {
	status, statusClass := "FAIL", "status-fail"
	if result.Ok {
		status, statusClass = "PASS", "status-pass"
	}
	return fmt.Sprintf(
		"<section class=\"script-result\">\n  <h2><span class=\"%s\">%s</span> %s</h2>\n  <p>%d of %d steps passed, %d ms</p>\n  %s\n  %s\n</section>",
		statusClass, status, escapeHTML(result.ScriptName),
		result.PassedSteps, result.TotalSteps, result.DurationMs,
		renderStepsTable(result.Steps), renderInlineScreenshots(byScope[scope], imageBase),
	)
}

func renderStepsTable(steps []runner.StepResult) string {
	if len(steps) == 0 {
		return "<p>No steps ran.</p>"
	}
	rows := make([]string, len(steps))
	for i, step := range steps {
		rowClass, status := "step-failed", "FAIL"
		if step.Ok {
			rowClass, status = "step-passed", "PASS"
		}
		detail := ""
		if step.Detail != nil && *step.Detail != "" {
			detail = escapeHTML(*step.Detail)
		}
		rows[i] = fmt.Sprintf(
			"<tr class=\"%s\">\n      <td>%d</td>\n      <td>%s</td>\n      <td>%s</td>\n      <td>%s</td>\n      <td>%d ms</td>\n    </tr>",
			rowClass, step.StepIndex+1, escapeHTML(step.Command), status, detail, step.DurationMs,
		)
	}
	return fmt.Sprintf(
		"<table class=\"steps\">\n    <thead><tr><th>Index</th><th>Command</th><th>Status</th><th>Detail</th><th>Duration</th></tr></thead>\n    <tbody>\n%s\n    </tbody>\n  </table>",
		strings.Join(rows, "\n"),
	)
}

func renderScenarioSection(result scenario.ScenarioResult, byScope ScreenshotsByScope, imageBase string) string {
	status, statusClass := "FAIL", "status-fail"
	if result.Ok {
		status, statusClass = "PASS", "status-pass"
	}
	children := make([]string, len(result.Scripts))
	for i, child := range result.Scripts {
		children[i] = renderScenarioChild(child, byScope, ScenarioChildScope(RootScriptScope, child.ScriptIndex, child.ScriptPath), imageBase)
	}
	return fmt.Sprintf(
		"<section class=\"scenario-result\">\n  <h2><span class=\"%s\">%s</span> %s</h2>\n  <p>%d of %d scripts passed, %d ms</p>\n  %s\n</section>",
		statusClass, status, escapeHTML(result.ScenarioName),
		result.PassedScripts, result.TotalScripts, result.DurationMs,
		strings.Join(children, "\n"),
	)
}

func renderScenarioChild(child scenario.ScenarioScriptResult, byScope ScreenshotsByScope, scope string, imageBase string) string {
	statusClass := scenarioChildStatusClass(child.Status)
	header := fmt.Sprintf(
		"<h3><span class=\"%s\">%s</span> [%d] %s</h3>",
		statusClass, escapeHTML(string(child.Status)), child.ScriptIndex+1, escapeHTML(child.ScriptPath),
	)
	if child.Error != nil {
		return fmt.Sprintf(
			"<article class=\"scenario-child\">\n  %s\n  <p class=\"error\">%s</p>\n</article>",
			header, escapeHTML(*child.Error),
		)
	}
	if child.Result == nil {
		return fmt.Sprintf("<article class=\"scenario-child\">\n  %s\n</article>", header)
	}
	return fmt.Sprintf(
		"<article class=\"scenario-child\">\n  %s\n  %s\n  %s\n</article>",
		header, renderStepsTable(child.Result.Steps), renderInlineScreenshots(byScope[scope], imageBase),
	)
}

// scenarioChildStatusClass maps a scenario child's run status to its
// style class. Skipped and errored each get their own class, distinct
// from a plain failure: skipped means the child never ran (an earlier
// sibling stopped the scenario), and errored means the run attempt
// itself failed before producing a pass or fail result.
func scenarioChildStatusClass(status scenario.ScriptRunStatus) string {
	switch status {
	case scenario.StatusPassed:
		return "status-pass"
	case scenario.StatusSkipped:
		return "status-skipped"
	case scenario.StatusErrored:
		return "status-errored"
	default:
		return "status-fail"
	}
}

func renderSuiteSection(result scenario.SuiteResult, entryVideos [][]string, byScope ScreenshotsByScope, imageBase string) string {
	status, statusClass := "FAIL", "status-fail"
	if result.Ok {
		status, statusClass = "PASS", "status-pass"
	}
	entries := make([]string, len(result.Entries))
	for i, entry := range result.Entries {
		entries[i] = renderSuiteEntry(entry, i, videosOf(entryVideos, i), byScope, imageBase)
	}
	suiteName := "suite"
	if result.SuiteName != nil {
		suiteName = *result.SuiteName
	}
	return fmt.Sprintf(
		"<section class=\"suite-result\">\n  <h2><span class=\"%s\">%s</span> %s</h2>\n  <p>%d of %d flows passed, %d failed%s, %d ms</p>\n  %s%s\n</section>",
		statusClass, status, escapeHTML(suiteName),
		result.Passed, result.Total, result.Failed, suiteCountsNote(result), result.DurationMs,
		strings.Join(entries, "\n"), renderQuarantinedSection(result),
	)
}

func renderSuiteEntry(entry scenario.SuiteEntryResult, entryIndex int, videos []string, byScope ScreenshotsByScope, imageBase string) string {
	scope := SuiteAttemptScope(SuiteEntryScope(entryIndex, entry.Path), finalAttemptNumber(entry))
	statusClass := "status-fail"
	if entry.Status == scenario.EntryPassed {
		statusClass = "status-pass"
	}
	header := fmt.Sprintf(
		"<h3><span class=\"%s\">%s</span> [%s] %s%s</h3>",
		statusClass, escapeHTML(string(entry.Status)), escapeHTML(string(entry.Kind)), escapeHTML(entry.Path), entryBadges(entry),
	)
	header += renderAttemptList(entry)
	header += videoLinks(videos, imageBase)
	if entry.Error != nil {
		return fmt.Sprintf(
			"<article class=\"suite-entry\">\n  %s\n  <p class=\"error\">%s</p>\n</article>",
			header, escapeHTML(*entry.Error),
		)
	}
	if entry.Result == nil {
		return fmt.Sprintf("<article class=\"suite-entry\">\n  %s\n</article>", header)
	}
	if entry.Kind == scenario.KindScenario && entry.Result.Scenario != nil {
		s := entry.Result.Scenario
		children := make([]string, len(s.Scripts))
		for i, child := range s.Scripts {
			children[i] = renderScenarioChild(child, byScope, ScenarioChildScope(scope, child.ScriptIndex, child.ScriptPath), imageBase)
		}
		return fmt.Sprintf(
			"<article class=\"suite-entry\">\n  %s\n  <p>%d of %d scripts passed, %d ms</p>\n  %s\n</article>",
			header, s.PassedScripts, s.TotalScripts, s.DurationMs, strings.Join(children, "\n"),
		)
	}
	if entry.Result.Script == nil {
		return fmt.Sprintf("<article class=\"suite-entry\">\n  %s\n</article>", header)
	}
	script := entry.Result.Script
	return fmt.Sprintf(
		"<article class=\"suite-entry\">\n  %s\n  %s\n  %s\n</article>",
		header, renderStepsTable(script.Steps), renderInlineScreenshots(byScope[scope], imageBase),
	)
}

func renderInlineScreenshots(refs []ScreenshotRef, imageBase string) string {
	if len(refs) == 0 {
		return ""
	}
	figures := make([]string, len(refs))
	for i, ref := range refs {
		figures[i] = fmt.Sprintf(
			"<figure>\n      <img src=\"%s%s\" alt=\"%s\">\n      <figcaption>%s</figcaption>\n    </figure>",
			imageBase, encodeURIComponent(ref.File), escapeHTML(ref.Label), escapeHTML(ref.Label),
		)
	}
	return fmt.Sprintf("<div class=\"screenshots\">\n%s\n  </div>", strings.Join(figures, "\n"))
}

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#39;",
)

func escapeHTML(value string) string {
	return htmlEscaper.Replace(value)
}

// encodeURIComponent mirrors JavaScript's encodeURIComponent: every
// byte is percent-encoded except the unreserved set
// A-Z a-z 0-9 - _ . ! ~ * ' ( ). Screenshot file names come from
// BuildScreenshotIndex's own sanitizer and never need escaping in
// practice, but this keeps the HTML link generation byte-for-byte
// faithful for any caller that constructs a ReportScreenshot with an
// unusual label.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreservedURIComponentByte(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isUnreservedURIComponentByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}

const style = `
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  margin: 0;
  padding: 24px;
  background: #f7f7f8;
  color: #1a1a1a;
}
h1, h2, h3 { margin: 0 0 8px; }
.status-pass { color: #0a7d34; font-weight: 700; }
.status-fail { color: #b3261e; font-weight: 700; }
.status-skipped { color: #6b6b6b; font-weight: 700; font-style: italic; }
.status-errored { color: #b35c00; font-weight: 700; }
.report-header {
  background: #ffffff;
  border-radius: 8px;
  padding: 16px 20px;
  margin-bottom: 20px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}
.meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 8px 16px;
  margin: 12px 0 0;
}
.meta dt { font-size: 12px; text-transform: uppercase; color: #666666; }
.meta dd { margin: 0; font-size: 14px; }
section, article {
  background: #ffffff;
  border-radius: 8px;
  padding: 16px 20px;
  margin-bottom: 16px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}
article.scenario-child, article.suite-entry { margin-top: 12px; }
table.steps { width: 100%; border-collapse: collapse; margin-top: 8px; }
table.steps th, table.steps td {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid #e5e5e5;
  font-size: 13px;
}
tr.step-failed { background: #fdecea; }
p.error { color: #b3261e; }
.screenshots { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 12px; }
.screenshots figure { margin: 0; width: 200px; }
.screenshots img { max-width: 100%; border-radius: 4px; border: 1px solid #dddddd; }
.screenshots figcaption { font-size: 12px; color: #555555; margin-top: 4px; word-break: break-word; }
`

// recordingRow renders the header row of a run's screen recording: a link
// per kept video file, or the reason no video was kept.
func recordingRow(recording ReportRecording, imageBase string) string {
	if recording.State == "kept" && len(recording.Files) > 0 {
		return fmt.Sprintf("<div><dt>Video</dt><dd>%s</dd></div>", videoAnchors(recording.Files, imageBase))
	}
	value := recording.State
	if recording.Code != "" {
		value = fmt.Sprintf("%s (%s)", recording.State, recording.Code)
	}
	return metaRow("Video", value)
}

// videoLinks renders the video paragraph of a suite entry, or nothing
// when the entry kept no video.
func videoLinks(files []string, imageBase string) string {
	if len(files) == 0 {
		return ""
	}
	return "\n  <p class=\"videos\">Video: " + videoAnchors(files, imageBase) + "</p>"
}

// videoAnchors renders one link per video file, oldest first.
func videoAnchors(files []string, imageBase string) string {
	links := make([]string, len(files))
	for i, file := range files {
		links[i] = fmt.Sprintf("<a href=\"%s%s\">%s</a>", imageBase, encodeURIComponent(file), escapeHTML(file))
	}
	return strings.Join(links, ", ")
}
