package mcp

import (
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
)

// runSummaryWire is one entry of the list_runs payload.
//
// Status is the original run verdict field kept for existing agents:
// passed, failed, error, cancelled, or running. Result and Outcome are
// the authoritative verdict. Result is one of passed, failed, or
// cancelled, and Outcome is the precise reason the run ended. A run
// that is still running has neither, so both are omitted. A run that
// never completed and has no live writer reports status cancelled,
// result cancelled, and outcome interrupted.
type runSummaryWire struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	Status        storage.RunStatus `json:"status"`
	Result        runfolder.Result  `json:"result,omitempty"`
	Outcome       runfolder.Outcome `json:"outcome,omitempty"`
	StartedAt     int64             `json:"startedAt"`
	FinishedAt    *int64            `json:"finishedAt,omitempty"`
	DurationMs    int64             `json:"durationMs"`
	ActionCount   int               `json:"actionCount"`
	FindingsCount int               `json:"findingsCount"`
}

// runDetailWire is the run object of the get_run payload. It carries
// the list_runs fields except findingsCount, plus the findings and the
// summary.
type runDetailWire struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Source      string                 `json:"source"`
	Status      storage.RunStatus      `json:"status"`
	Result      runfolder.Result       `json:"result,omitempty"`
	Outcome     runfolder.Outcome      `json:"outcome,omitempty"`
	StartedAt   int64                  `json:"startedAt"`
	FinishedAt  *int64                 `json:"finishedAt,omitempty"`
	DurationMs  int64                  `json:"durationMs"`
	ActionCount int                    `json:"actionCount"`
	Findings    []storage.RunBugReport `json:"findings"`
	Summary     *string                `json:"summary,omitempty"`
}

// runNameOf returns the display name of a run: the MCP name when the
// run carries one, otherwise the script name.
func runNameOf(m runfolder.Meta) string {
	if m.MCP != nil {
		return m.MCP.Name
	}
	return m.ScriptName
}

func runSourceOf(m runfolder.Meta) string {
	if m.MCP != nil {
		return m.MCP.Source
	}
	return string(m.Kind)
}

func runDurationMs(m runfolder.Meta) int64 {
	if m.MCP != nil {
		return m.MCP.DurationMs
	}
	if m.CompletedAt != nil {
		return *m.CompletedAt - m.StartedAt
	}
	return 0
}

func runActionCount(m runfolder.Meta) int {
	if m.MCP != nil {
		return m.MCP.ActionCount
	}
	return 0
}

func runFindings(m runfolder.Meta) []storage.RunBugReport {
	findings := []storage.RunBugReport{}
	if m.MCP == nil {
		return findings
	}
	for _, f := range m.MCP.Findings {
		findings = append(findings, storage.RunBugReport{
			ID: f.ID, Title: f.Title, Description: f.Description,
			ScreenshotPath: f.ScreenshotPath, Timestamp: f.Timestamp,
		})
	}
	return findings
}

func summaryWireOf(m runfolder.Meta) runSummaryWire {
	return runSummaryWire{
		ID: m.RunID, Name: runNameOf(m), Source: runSourceOf(m),
		Status: storage.RunStatus(runfolder.LegacyStatusOf(m)), Result: m.Result, Outcome: m.Outcome,
		StartedAt: m.StartedAt, FinishedAt: m.CompletedAt,
		DurationMs: runDurationMs(m), ActionCount: runActionCount(m),
		FindingsCount: len(runFindings(m)),
	}
}

func detailWireOf(m runfolder.Meta) runDetailWire {
	var summary *string
	if m.MCP != nil {
		summary = m.MCP.Summary
	}
	return runDetailWire{
		ID: m.RunID, Name: runNameOf(m), Source: runSourceOf(m),
		Status: storage.RunStatus(runfolder.LegacyStatusOf(m)), Result: m.Result, Outcome: m.Outcome,
		StartedAt: m.StartedAt, FinishedAt: m.CompletedAt,
		DurationMs: runDurationMs(m), ActionCount: runActionCount(m),
		Findings: runFindings(m), Summary: summary,
	}
}
