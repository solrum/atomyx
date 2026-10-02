package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var startRunInputSchema = jsObject(map[string]any{
	"name":   jsStringDesc("Human description of the run."),
	"source": jsString(),
}, "name")

var startRunSchema = compileToolSchema("start_run", startRunInputSchema)

type startRunArgs struct {
	Name   string  `json:"name"`
	Source *string `json:"source,omitempty"`
}

type startRunResult struct {
	Ok                 bool    `json:"ok"`
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Source             string  `json:"source"`
	StartedAt          int64   `json:"startedAt"`
	ErroredPredecessor *string `json:"erroredPredecessor,omitempty"`
}

// registerStartRunTool adds the start_run tool: begins a new test
// run in its own run folder, force-closing any previously-active run
// first.
func registerStartRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "start_run",
		Description: "Start a new test run. Call this BEFORE driving the device so bug reports " +
			"and case studies can be attached to a named run. Any previously-active run " +
			"is implicitly force-closed with outcome 'error' and its run folder is " +
			"finalized, so the lost run's trail isn't dropped. The returned " +
			"`erroredPredecessor` field carries the old run id when this happens.",
		InputSchema: startRunInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(startRunSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("start_run", err), nil
		}
		var args startRunArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("start_run", err), nil
		}

		started, err := tc.RunSession.Start(args.Name, args.Source, tc.Session.SelectedDescriptor())
		if err != nil {
			return errorResult(err), nil
		}
		run := started.Run

		var erroredPredecessorID *string
		if predecessor := started.Superseded; predecessor != nil {
			id := predecessor.ID
			erroredPredecessorID = &id
		}

		tc.Logger.Info("run.started", logging.LogContext{"id": run.ID, "name": run.Name})
		return jsonToolResult(startRunResult{
			Ok:                 true,
			ID:                 run.ID,
			Name:               run.Name,
			Source:             run.Source,
			StartedAt:          run.StartedAt,
			ErroredPredecessor: erroredPredecessorID,
		})
	})
}

var finishRunInputSchema = jsObject(map[string]any{
	"status":  jsAnyOf(jsConst("passed"), jsConst("failed"), jsConst("error")),
	"summary": jsString(),
})

var finishRunSchema = compileToolSchema("finish_run", finishRunInputSchema)

type finishRunArgs struct {
	Status  *string `json:"status,omitempty"`
	Summary *string `json:"summary,omitempty"`
}

type finishRunResult struct {
	Ok            bool              `json:"ok"`
	ID            string            `json:"id"`
	Status        storage.RunStatus `json:"status"`
	ActionCount   int               `json:"actionCount"`
	FindingsCount int               `json:"findingsCount"`
	DurationMs    int64             `json:"durationMs"`
}

// registerFinishRunTool adds the finish_run tool: closes the active
// run with a verdict and finalizes its run folder.
func registerFinishRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "finish_run",
		Description: "Finish the currently-active test run with a verdict and optional summary. " +
			"Finalizes the run's folder, with outcome passed, failed, or error to match " +
			"the status, so it can be read back with get_run. Returns { ok, id, status, actionCount, findingsCount, durationMs }.",
		InputSchema: finishRunInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(finishRunSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("finish_run", err), nil
		}
		var args finishRunArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("finish_run", err), nil
		}
		status := ""
		if args.Status != nil {
			status = *args.Status
		}

		finished := tc.RunSession.Finish(status, args.Summary)
		run := finished.Run
		if run == nil {
			return jsonToolResult(map[string]any{"ok": false, "reason": "no active run to finish"})
		}
		tc.Logger.Info("run.finished", logging.LogContext{
			"id":       run.ID,
			"status":   string(run.Status),
			"duration": finished.DurationMs,
		})
		return jsonToolResult(finishRunResult{
			Ok:            true,
			ID:            run.ID,
			Status:        run.Status,
			ActionCount:   run.ActionCount,
			FindingsCount: len(run.Findings),
			DurationMs:    finished.DurationMs,
		})
	})
}
