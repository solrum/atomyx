package report

import (
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
)

func failedScript() runner.ScriptResult {
	return runner.ScriptResult{ScriptName: "Login", Steps: []runner.StepResult{{StepIndex: 0, Command: "tap"}}, TotalSteps: 1}
}

func TestRecordingRender_ScriptReportLinksKeptFilesInHTMLAndJSON(t *testing.T) {
	input := ScriptReportInput{
		Metadata: ReportMetadata{
			RunID: "r1", Label: "Login", Command: ReportCommandRun,
			Recording: &ReportRecording{State: "kept", Files: []string{"video-run-1.mp4", "video-run-2.mp4"}},
		},
		Result: failedScript(),
	}
	html, err := RenderHTML(input, nil, "../artifacts/")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	for _, want := range []string{`href="../artifacts/video-run-1.mp4"`, `href="../artifacts/video-run-2.mp4"`} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %s:\n%s", want, html)
		}
	}
	rendered, err := RenderJSON(input, nil)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	body, _ := json.Marshal(rendered.Metadata)
	if !strings.Contains(string(body), `"recording":{"state":"kept","files":["video-run-1.mp4","video-run-2.mp4"]}`) {
		t.Errorf("json metadata does not list the files: %s", body)
	}
}

func TestRecordingRender_UnsupportedStateShowsNoLinkAndOmittedWhenAbsent(t *testing.T) {
	input := ScriptReportInput{
		Metadata: ReportMetadata{RunID: "r1", Label: "Login", Command: ReportCommandRun,
			Recording: &ReportRecording{State: "unsupported", Code: "RECORDING_UNSUPPORTED"}},
		Result: failedScript(),
	}
	html, _ := RenderHTML(input, nil, "../artifacts/")
	if !strings.Contains(html, "unsupported (RECORDING_UNSUPPORTED)") || strings.Contains(html, "<a href") {
		t.Errorf("unsupported recording rendered wrongly:\n%s", html)
	}
	input.Metadata.Recording = nil
	html, _ = RenderHTML(input, nil, "../artifacts/")
	if strings.Contains(html, "Video") {
		t.Errorf("a run with no recording mentions video:\n%s", html)
	}
	rendered, _ := RenderJSON(input, nil)
	body, _ := json.Marshal(rendered.Metadata)
	if strings.Contains(string(body), "recording") {
		t.Errorf("a run with no recording has a recording field: %s", body)
	}
}

func TestRecordingRender_SuiteListsKeptFilesPerEntry(t *testing.T) {
	failed := "boom"
	input := SuiteReportInput{
		Metadata: ReportMetadata{RunID: "s1", Label: "suite", Command: ReportCommandSuite},
		Result: scenario.SuiteResult{Total: 2, Failed: 1, Passed: 1, Entries: []scenario.SuiteEntryResult{
			{Path: "a.yml", Kind: scenario.KindScript, Status: scenario.EntryPassed},
			{Path: "b.yml", Kind: scenario.KindScript, Status: scenario.EntryFailed, Error: &failed},
		}},
		EntryVideos: [][]string{nil, {"video-entry-2-1.mp4"}},
	}
	html, err := RenderHTML(input, nil, "../artifacts/")
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if got := strings.Count(html, "video-entry-2-1.mp4"); got != 2 {
		t.Errorf("want the link and its label once each, got %d:\n%s", got, html)
	}
	rendered, _ := RenderJSON(input, nil)
	suite := rendered.Result.(ReportJsonSuiteResult)
	if len(suite.Entries[0].Videos) != 0 || len(suite.Entries[1].Videos) != 1 || suite.Entries[1].Videos[0] != "video-entry-2-1.mp4" {
		t.Errorf("entry videos = %v, %v", suite.Entries[0].Videos, suite.Entries[1].Videos)
	}
}
