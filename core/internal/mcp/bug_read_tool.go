package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// isTopLevelBugKey reports whether key is a top-level bug record
// ("bugs/abc-1") rather than a nested sub-record ("bugs/abc-1/screenshot").
func isTopLevelBugKey(key string) bool {
	const prefix = "bugs/"
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	return !strings.Contains(key[len(prefix):], "/")
}

var listBugsInputSchema = jsObject(map[string]any{
	"runId":  jsString(),
	"limit":  jsNumber(),
	"offset": jsNumber(),
})

var listBugsSchema = compileToolSchema("list_bugs", listBugsInputSchema)

type listBugsArgs struct {
	RunID  *string `json:"runId,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Offset *int    `json:"offset,omitempty"`
}

type bugSummaryWire struct {
	ID            string `json:"id"`
	RunID         string `json:"runId"`
	RunName       string `json:"runName,omitempty"`
	Title         string `json:"title"`
	Timestamp     int64  `json:"timestamp"`
	HasScreenshot bool   `json:"hasScreenshot"`
}

type listBugsResult struct {
	Ok            bool             `json:"ok"`
	Count         int              `json:"count"`
	TotalMatching int              `json:"totalMatching"`
	Offset        int              `json:"offset"`
	NextOffset    *int             `json:"nextOffset"`
	Bugs          []bugSummaryWire `json:"bugs"`
}

// registerListBugsTool adds the list_bugs tool: paginated bug
// summaries sorted by timestamp descending, optionally filtered by
// run id.
func registerListBugsTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "list_bugs",
		Description: "List persisted bug reports from storage, sorted by timestamp " +
			"descending (newest first). Returns summaries only. Use get_bug " +
			"for the full record. Supports runId filter and offset/limit " +
			"pagination. Response includes `totalMatching` + `nextOffset` for " +
			"easy paging.",
		InputSchema: listBugsInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(listBugsSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("list_bugs", err), nil
		}
		var args listBugsArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("list_bugs", err), nil
		}
		limit := 50
		if args.Limit != nil {
			limit = *args.Limit
		}
		offset := 0
		if args.Offset != nil {
			offset = *args.Offset
		}

		keys, err := tc.Storage.List("bugs")
		if err != nil {
			return errorResult(err), nil
		}
		var records []storage.PersistedBug
		for _, key := range keys {
			if !isTopLevelBugKey(key) {
				continue
			}
			var record storage.PersistedBug
			ok, err := tc.Storage.Load(key, &record)
			if err != nil {
				return errorResult(err), nil
			}
			if ok {
				records = append(records, record)
			}
		}

		filtered := records
		if args.RunID != nil {
			filtered = filtered[:0]
			for _, r := range records {
				if r.RunID == *args.RunID {
					filtered = append(filtered, r)
				}
			}
		}
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Timestamp > filtered[j].Timestamp })

		page := paginate(filtered, offset, limit)
		summaries := make([]bugSummaryWire, len(page))
		for i, b := range page {
			summaries[i] = bugSummaryWire{
				ID:            b.ID,
				RunID:         b.RunID,
				RunName:       b.RunName,
				Title:         b.Title,
				Timestamp:     b.Timestamp,
				HasScreenshot: b.ScreenshotPath != nil,
			}
		}
		result := listBugsResult{
			Ok:            true,
			Count:         len(summaries),
			TotalMatching: len(filtered),
			Offset:        offset,
			NextOffset:    nextOffset(offset, len(summaries), len(filtered)),
			Bugs:          summaries,
		}
		if result.Bugs == nil {
			result.Bugs = []bugSummaryWire{}
		}
		return jsonToolResult(result)
	})
}

// nextOffset computes list-tools' pagination cursor: nil when the
// page reached the end of the filtered set, otherwise the offset of
// the next page.
func nextOffset(offset, pageLen, totalLen int) *int {
	if offset+pageLen < totalLen {
		next := offset + pageLen
		return &next
	}
	return nil
}

// paginate returns the slice of items starting at offset, up to limit
// long. An out-of-range offset returns an empty slice, matching
// JavaScript's Array.prototype.slice semantics for a start past the
// array's end.
func paginate[T any](items []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

var getBugInputSchema = jsObject(map[string]any{
	"id": jsStringDesc("Bug id as returned by report_bug."),
}, "id")

var getBugSchema = compileToolSchema("get_bug", getBugInputSchema)

type bugIDArgs struct {
	ID string `json:"id"`
}

// registerGetBugTool adds the get_bug tool: fetches one persisted bug
// record by id.
func registerGetBugTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "get_bug",
		Description: "Fetch a single persisted bug report by id. Returns the full record " +
			"including description and screenshotPath. The screenshot bytes live " +
			"under the returned screenshotPath key and can be loaded separately by " +
			"consumers that need them. Returns ok:false when the id is not found.",
		InputSchema: getBugInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(getBugSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("get_bug", err), nil
		}
		var args bugIDArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("get_bug", err), nil
		}
		var record storage.PersistedBug
		ok, err := tc.Storage.Load(storage.BugKey(args.ID), &record)
		if err != nil {
			return errorResult(err), nil
		}
		if !ok {
			return jsonToolResult(map[string]any{"ok": false, "reason": fmt.Sprintf("bug not found: %s", args.ID)})
		}
		return jsonToolResult(map[string]any{"ok": true, "bug": record})
	})
}

var deleteBugInputSchema = jsObject(map[string]any{
	"id": jsStringDesc("Bug id to delete."),
}, "id")

var deleteBugSchema = compileToolSchema("delete_bug", deleteBugInputSchema)

// registerDeleteBugTool adds the delete_bug tool: removes a bug
// record and its sibling screenshot key, if any.
func registerDeleteBugTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "delete_bug",
		Description: "Delete a persisted bug report. Removes BOTH `bugs/<id>` and its " +
			"sibling `bugs/<id>/screenshot` key when present, so no orphan " +
			"screenshot payloads are left behind. Returns ok:false when the id " +
			"is not found (before any delete happens, so partial-delete states " +
			"are impossible).",
		InputSchema: deleteBugInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(deleteBugSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("delete_bug", err), nil
		}
		var args bugIDArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("delete_bug", err), nil
		}
		bugKey := storage.BugKey(args.ID)
		var existing storage.PersistedBug
		ok, err := tc.Storage.Load(bugKey, &existing)
		if err != nil {
			return errorResult(err), nil
		}
		if !ok {
			return jsonToolResult(map[string]any{"ok": false, "reason": fmt.Sprintf("bug not found: %s", args.ID)})
		}
		if err := tc.Storage.Delete(bugKey); err != nil {
			return errorResult(err), nil
		}
		if err := tc.Storage.Delete(bugKey + "/screenshot"); err != nil {
			return errorResult(err), nil
		}
		tc.Logger.Info("bug.deleted", logging.LogContext{"id": args.ID})
		return jsonToolResult(map[string]any{"ok": true, "id": args.ID})
	})
}
