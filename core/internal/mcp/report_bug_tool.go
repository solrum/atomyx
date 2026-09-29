package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var reportBugInputSchema = jsObject(map[string]any{
	"title": jsStringDesc("One-line bug title."),
	"description": jsStringDesc("Full description: expected vs actual, repro steps, any context " +
		"the agent gathered (selectors tried, tool results, etc.)."),
	"captureScreenshot": jsBoolean(),
}, "title", "description")

var reportBugSchema = compileToolSchema("report_bug", reportBugInputSchema)

type reportBugArgs struct {
	Title             string `json:"title"`
	Description       string `json:"description"`
	CaptureScreenshot *bool  `json:"captureScreenshot,omitempty"`
}

type screenshotRecord struct {
	Base64     string `json:"base64"`
	Format     string `json:"format"`
	CapturedAt int64  `json:"capturedAt"`
}

type reportBugResult struct {
	Ok             bool    `json:"ok"`
	ID             string  `json:"id"`
	RunID          string  `json:"runId"`
	ScreenshotPath *string `json:"screenshotPath,omitempty"`
}

// registerReportBugTool adds the report_bug tool: records a bug
// finding against the active run and optionally captures a
// screenshot as evidence. It deliberately does not require an active
// device. Losing a bug report because the device went away is worse
// than losing the screenshot, so a missing device only skips the
// screenshot step.
func registerReportBugTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "report_bug",
		Description: "Record a bug finding in the currently-active run. Optionally captures " +
			"a screenshot. Use this AFTER confirming the bug by observing the device " +
			"state. Returns the bug id for reference.",
		InputSchema: reportBugInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(reportBugSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("report_bug", err), nil
		}
		var args reportBugArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("report_bug", err), nil
		}

		current := tc.RunStore.Current()
		if current == nil {
			return jsonToolResult(map[string]any{
				"ok":     false,
				"reason": "no active run, so call start_run first before reporting bugs",
			})
		}

		bug, err := tc.RunStore.RecordBug(storage.RecordBugOptions{
			Title:       args.Title,
			Description: args.Description,
		})
		if err != nil {
			return errorResult(err), nil
		}

		var screenshotPath *string
		if args.CaptureScreenshot == nil || *args.CaptureScreenshot {
			active := tc.Session.Current()
			if active == nil {
				tc.Logger.Warn("report_bug: no active device, skipping screenshot capture", logging.LogContext{
					"bugId": bug.ID,
				})
			} else if data, err := active.Orchestra.Screenshot(ctx); err != nil {
				tc.Logger.Warn("report_bug: screenshot failed", logging.LogContext{"error": err.Error()})
			} else {
				key := storage.BugKey(bug.ID) + "/screenshot"
				record := screenshotRecord{
					Base64:     base64.StdEncoding.EncodeToString(data),
					Format:     "png",
					CapturedAt: tc.Clock.NowMs(),
				}
				if err := tc.Storage.Save(key, record); err != nil {
					tc.Logger.Warn("report_bug: screenshot failed", logging.LogContext{"error": err.Error()})
				} else {
					screenshotPath = &key
				}
			}
		}

		persisted := storage.PersistedBug{
			ID:             bug.ID,
			RunID:          current.ID,
			RunName:        current.Name,
			Title:          bug.Title,
			Description:    bug.Description,
			ScreenshotPath: screenshotPath,
			Timestamp:      bug.Timestamp,
		}
		if err := tc.Storage.Save(storage.BugKey(bug.ID), persisted); err != nil {
			return errorResult(err), nil
		}

		tc.Logger.Info("bug.reported", logging.LogContext{"id": bug.ID, "title": bug.Title})
		return jsonToolResult(reportBugResult{
			Ok:             true,
			ID:             bug.ID,
			RunID:          current.ID,
			ScreenshotPath: screenshotPath,
		})
	})
}
