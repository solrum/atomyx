package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listRunsInputSchema = jsObject(map[string]any{
	"status": jsAnyOf(jsConst("passed"), jsConst("failed"), jsConst("error"), jsConst("running")),
	"limit":  jsNumber(),
	"offset": jsNumber(),
})

var listRunsSchema = compileToolSchema("list_runs", listRunsInputSchema)

type listRunsArgs struct {
	Status *string `json:"status,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Offset *int    `json:"offset,omitempty"`
}

type runSummaryWire struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Source        string            `json:"source"`
	Status        storage.RunStatus `json:"status"`
	StartedAt     int64             `json:"startedAt"`
	FinishedAt    *int64            `json:"finishedAt,omitempty"`
	DurationMs    int64             `json:"durationMs"`
	ActionCount   int               `json:"actionCount"`
	FindingsCount int               `json:"findingsCount"`
}

type listRunsResult struct {
	Ok            bool             `json:"ok"`
	Count         int              `json:"count"`
	TotalMatching int              `json:"totalMatching"`
	Offset        int              `json:"offset"`
	NextOffset    *int             `json:"nextOffset"`
	Runs          []runSummaryWire `json:"runs"`
}

func loadPersistedRuns(store storage.Storage) ([]storage.PersistedRun, error) {
	keys, err := store.List("runs")
	if err != nil {
		return nil, err
	}
	var records []storage.PersistedRun
	for _, key := range keys {
		var record storage.PersistedRun
		ok, err := store.Load(key, &record)
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	return records, nil
}

// registerListRunsTool adds the list_runs tool: paginated run
// summaries sorted by start time descending, optionally filtered by
// status.
func registerListRunsTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "list_runs",
		Description: "List persisted runs from storage, sorted by startedAt descending " +
			"(most recent first). Returns summaries only. Use get_run to fetch the " +
			"full record including findings. Supports status filter and " +
			"offset/limit pagination. The response includes `totalMatching` so " +
			"callers can decide whether to request another page.",
		InputSchema: listRunsInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(listRunsSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("list_runs", err), nil
		}
		var args listRunsArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("list_runs", err), nil
		}
		limit := 20
		if args.Limit != nil {
			limit = *args.Limit
		}
		offset := 0
		if args.Offset != nil {
			offset = *args.Offset
		}

		records, err := loadPersistedRuns(tc.Storage)
		if err != nil {
			return errorResult(err), nil
		}
		filtered := records
		if args.Status != nil {
			filtered = filtered[:0]
			for _, r := range records {
				if string(r.Status) == *args.Status {
					filtered = append(filtered, r)
				}
			}
		}
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].StartedAt > filtered[j].StartedAt })

		page := paginate(filtered, offset, limit)
		summaries := make([]runSummaryWire, len(page))
		for i, r := range page {
			summaries[i] = runSummaryWire{
				ID:            r.ID,
				Name:          r.Name,
				Source:        r.Source,
				Status:        r.Status,
				StartedAt:     r.StartedAt,
				FinishedAt:    r.FinishedAt,
				DurationMs:    r.DurationMs,
				ActionCount:   r.ActionCount,
				FindingsCount: len(r.Findings),
			}
		}
		result := listRunsResult{
			Ok:            true,
			Count:         len(summaries),
			TotalMatching: len(filtered),
			Offset:        offset,
			NextOffset:    nextOffset(offset, len(summaries), len(filtered)),
			Runs:          summaries,
		}
		if result.Runs == nil {
			result.Runs = []runSummaryWire{}
		}
		return jsonToolResult(result)
	})
}

var getRunInputSchema = jsObject(map[string]any{
	"id": jsStringDesc("Run id as returned by start_run."),
}, "id")

var getRunSchema = compileToolSchema("get_run", getRunInputSchema)

type runIDArgs struct {
	ID string `json:"id"`
}

// registerGetRunTool adds the get_run tool: fetches one persisted run
// by id.
func registerGetRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "get_run",
		Description: "Fetch a single persisted run by id. Returns the full record " +
			"including findings and summary. Use list_runs first to discover " +
			"available ids. Returns ok:false when the id is not found.",
		InputSchema: getRunInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(getRunSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("get_run", err), nil
		}
		var args runIDArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("get_run", err), nil
		}
		var record storage.PersistedRun
		ok, err := tc.Storage.Load(storage.RunKey(args.ID), &record)
		if err != nil {
			return errorResult(err), nil
		}
		if !ok {
			return jsonToolResult(map[string]any{"ok": false, "reason": fmt.Sprintf("run not found: %s", args.ID)})
		}
		return jsonToolResult(map[string]any{"ok": true, "run": record})
	})
}

var deleteRunInputSchema = jsObject(map[string]any{
	"id": jsStringDesc("Run id to delete."),
}, "id")

var deleteRunSchema = compileToolSchema("delete_run", deleteRunInputSchema)

// registerDeleteRunTool adds the delete_run tool: removes a persisted
// run record. Associated bug records are not cascade-deleted.
func registerDeleteRunTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "delete_run",
		Description: "Delete a persisted run record. Removes `runs/<id>` from storage. " +
			"Associated bug records (`bugs/<runId>-bug-<n>`) are NOT cascade-" +
			"deleted, and they remain accessible via get_bug with their runId field " +
			"pointing at the now-missing run. Use list_bugs with the runId filter " +
			"then delete_bug to clean those up individually if desired. Returns " +
			"ok:false when the id is not found.",
		InputSchema: deleteRunInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(deleteRunSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("delete_run", err), nil
		}
		var args runIDArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("delete_run", err), nil
		}
		key := storage.RunKey(args.ID)
		var existing storage.PersistedRun
		ok, err := tc.Storage.Load(key, &existing)
		if err != nil {
			return errorResult(err), nil
		}
		if !ok {
			return jsonToolResult(map[string]any{"ok": false, "reason": fmt.Sprintf("run not found: %s", args.ID)})
		}
		if err := tc.Storage.Delete(key); err != nil {
			return errorResult(err), nil
		}
		tc.Logger.Info("run.deleted", logging.LogContext{"id": args.ID})
		return jsonToolResult(map[string]any{"ok": true, "id": args.ID})
	})
}

var updateRunSummaryInputSchema = jsObject(map[string]any{
	"id":      jsStringDesc("Run id to update."),
	"summary": jsStringDesc("New summary text. Pass an empty string to clear."),
}, "id", "summary")

var updateRunSummarySchema = compileToolSchema("update_run_summary", updateRunSummaryInputSchema)

type updateRunSummaryArgs struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// registerUpdateRunSummaryTool adds the update_run_summary tool: sets
// or replaces the summary field on a persisted run, leaving every
// other field untouched.
func registerUpdateRunSummaryTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "update_run_summary",
		Description: "Set or replace the summary field on a persisted run. Useful when " +
			"the summary wasn't known at finish_run time, or to append context " +
			"discovered by a later investigation. Only the summary field is " +
			"mutated, while all other fields (status, findings, durationMs, …) are " +
			"preserved. Returns ok:false when the id is not found.",
		InputSchema: updateRunSummaryInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(updateRunSummarySchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("update_run_summary", err), nil
		}
		var args updateRunSummaryArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("update_run_summary", err), nil
		}
		key := storage.RunKey(args.ID)
		var existing storage.PersistedRun
		ok, err := tc.Storage.Load(key, &existing)
		if err != nil {
			return errorResult(err), nil
		}
		if !ok {
			return jsonToolResult(map[string]any{"ok": false, "reason": fmt.Sprintf("run not found: %s", args.ID)})
		}
		existing.Summary = &args.Summary
		if err := tc.Storage.Save(key, existing); err != nil {
			return errorResult(err), nil
		}
		tc.Logger.Info("run.summary_updated", logging.LogContext{"id": args.ID, "length": len(args.Summary)})
		return jsonToolResult(map[string]any{"ok": true, "id": args.ID, "summary": args.Summary})
	})
}
