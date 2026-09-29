package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const forceClosedPredecessorSummary = "force-closed by subsequent start_run, because the agent forgot finish_run"

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
// run, force-closing and persisting any previously-active run first.
func registerStartRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "start_run",
		Description: "Start a new test run. Call this BEFORE driving the device so bug reports " +
			"and case studies can be attached to a named run. Any previously-active run " +
			"is implicitly force-closed as 'error' and its record is persisted to " +
			"runs/<id> so the lost run's trail isn't dropped. The returned " +
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

		started := tc.RunStore.Start(storage.StartOptions{Name: args.Name, Source: args.Source})
		run := started.Run

		var erroredPredecessorID *string
		if predecessor := started.ErroredPredecessor; predecessor != nil {
			summary := forceClosedPredecessorSummary
			persisted := storage.NewPersistedRun(predecessor, &summary, tc.Clock.NowMs())
			if err := tc.Storage.Save(storage.RunKey(predecessor.ID), persisted); err != nil {
				return errorResult(err), nil
			}
			id := predecessor.ID
			erroredPredecessorID = &id
			tc.Logger.Warn("run.force_closed_predecessor", logging.LogContext{
				"id":       predecessor.ID,
				"newRunId": run.ID,
			})
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
// run with a verdict and persists the finished record.
func registerFinishRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "finish_run",
		Description: "Finish the currently-active test run with a verdict and optional summary. " +
			"Persists the run record to storage (key: runs/<id>) so it can be read back " +
			"or exported to a reporter. Returns { ok, id, status, actionCount, findingsCount, durationMs }.",
		InputSchema: finishRunInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(finishRunSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("finish_run", err), nil
		}
		var args finishRunArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("finish_run", err), nil
		}
		status := storage.RunStatusPassed
		if args.Status != nil {
			status = storage.RunStatus(*args.Status)
		}

		run := tc.RunStore.Finish(status)
		if run == nil {
			return jsonToolResult(map[string]any{"ok": false, "reason": "no active run to finish"})
		}
		persisted := storage.NewPersistedRun(run, args.Summary, tc.Clock.NowMs())
		if err := tc.Storage.Save(storage.RunKey(run.ID), persisted); err != nil {
			return errorResult(err), nil
		}
		tc.Logger.Info("run.finished", logging.LogContext{
			"id":       run.ID,
			"status":   string(run.Status),
			"duration": persisted.DurationMs,
		})
		return jsonToolResult(finishRunResult{
			Ok:            true,
			ID:            run.ID,
			Status:        run.Status,
			ActionCount:   run.ActionCount,
			FindingsCount: len(run.Findings),
			DurationMs:    persisted.DurationMs,
		})
	})
}
