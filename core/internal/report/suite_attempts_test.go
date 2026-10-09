package report

import (
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/scenario"
)

func retriedSuite() SuiteReportInput {
	return SuiteReportInput{
		Metadata:    ReportMetadata{RunID: "r1", Label: "nightly", Command: ReportCommandSuite},
		EntryVideos: [][]string{{"video-entry-1-1.mp4"}},
		Result: scenario.SuiteResult{
			Ok: true, Total: 4, Passed: 3, Flaky: 1, QuarantinedFailures: 1,
			Entries: []scenario.SuiteEntryResult{
				{
					Path: "a.yml", Kind: scenario.KindScript, Status: scenario.EntryPassed, Flaky: true,
					Attempts: []scenario.SuiteAttempt{
						{Number: 1, Status: scenario.EntryFailed, Videos: []string{"video-entry-1-1.mp4"}},
						{Number: 2, Status: scenario.EntryPassed},
					},
				},
				{
					Path: "b.yml", Kind: scenario.KindScript, Status: scenario.EntryFailed, Quarantined: true,
					Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryFailed}},
				},
				{Path: "c.yml", Kind: scenario.KindScript, Status: scenario.EntryPassed, Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryPassed}}},
				{Path: "d.yml", Kind: scenario.KindScript, Status: scenario.EntryPassed, Quarantined: true, Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryPassed}}},
			},
		},
	}
}

func TestSuiteAttemptScopeKeepsTheFirstAttemptScope(t *testing.T) {
	entry := SuiteEntryScope(3, "a.yml")
	if SuiteAttemptScope(entry, 0) != entry || SuiteAttemptScope(entry, 1) != entry {
		t.Error("the first attempt changed the entry scope")
	}
	if SuiteAttemptScope(entry, 2) == entry || SuiteAttemptScope(entry, 2) == SuiteAttemptScope(entry, 3) {
		t.Error("later attempts share a scope")
	}
}

func TestSuiteHTMLShowsFlakyBadgeAttemptsAndQuarantinedSection(t *testing.T) {
	html, err := RenderHTML(retriedSuite(), nil, "../artifacts/")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"flaky (2 attempts)",
		"Attempt 1: <span class=\"status-fail\">failed</span>",
		"Attempt 2: <span class=\"status-pass\">passed</span>",
		"Quarantined failures",
		"<li>b.yml</li>",
		"1 flaky, 1 quarantined",
		`href="../artifacts/video-entry-1-1.mp4"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %q", want)
		}
	}
	if strings.Contains(html, "<li>d.yml</li>") {
		t.Error("a quarantined entry that passed is listed as a quarantined failure")
	}
	if strings.Count(html, "flaky (") != 1 || strings.Count(html, ">quarantined<") != 1 {
		t.Errorf("badges appear on entries that are neither flaky nor quarantined")
	}
}

func TestSuiteHTMLOmitsAttemptMarkupForASuiteThatRanOnce(t *testing.T) {
	input := retriedSuite()
	input.Result = scenario.SuiteResult{Ok: true, Total: 1, Passed: 1, Entries: []scenario.SuiteEntryResult{
		{Path: "c.yml", Kind: scenario.KindScript, Status: scenario.EntryPassed, Attempts: []scenario.SuiteAttempt{{Number: 1, Status: scenario.EntryPassed}}},
	}}

	html, err := RenderHTML(input, nil, "../artifacts/")
	if err != nil {
		t.Fatal(err)
	}

	for _, banned := range []string{"flaky", "quarantine", "Attempt 1", "attempts"} {
		if strings.Contains(html, banned) {
			t.Errorf("html mentions %q for a suite without retries", banned)
		}
	}
}

func TestSuiteJSONCarriesAttemptsFlakyAndQuarantined(t *testing.T) {
	rendered, err := RenderJSON(retriedSuite(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rendered.Result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	for _, want := range []string{`"flaky":1`, `"quarantinedFailures":1`, `"flaky":true`, `"quarantined":true`, `"attempts":[{"number":1,"status":"failed"`} {
		if !strings.Contains(text, want) {
			t.Errorf("json misses %s: %s", want, text)
		}
	}
	if strings.Count(text, `"attempts"`) != 1 {
		t.Errorf("an entry that ran once lists attempts: %s", text)
	}
}
