package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"dev.atomyx/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const caseStudySlugMaxLen = 60

var addCaseStudyInputSchema = jsObject(map[string]any{
	"title":    jsString(),
	"trigger":  jsStringDesc("What situation caused the pain."),
	"solution": jsStringDesc("What worked to recover."),
	"example":  jsString(),
}, "title", "trigger", "solution")

var addCaseStudySchema = compileToolSchema("add_case_study", addCaseStudyInputSchema)

type addCaseStudyArgs struct {
	Title    string  `json:"title"`
	Trigger  string  `json:"trigger"`
	Solution string  `json:"solution"`
	Example  *string `json:"example,omitempty"`
}

// caseStudySlug derives a URL-safe and filename-safe slug from title:
// lowercase, collapse every run of non [a-z0-9] characters into a
// single dash, trim leading/trailing dashes, then cap the result at
// caseStudySlugMaxLen characters (the cap is applied last, so a slug
// that was exactly at the limit before trimming may end in a dash).
func caseStudySlug(title string) string {
	lower := strings.ToLower(title)
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > caseStudySlugMaxLen {
		slug = slug[:caseStudySlugMaxLen]
	}
	return slug
}

// registerAddCaseStudyTool adds the add_case_study tool: appends a
// markdown lesson under case-studies/YYYY-MM/<slug>.
func registerAddCaseStudyTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "add_case_study",
		Description: "Append a learned lesson to the case-studies playbook. Use AFTER recovering " +
			"from a non-obvious error (obscurement dismissal, scroll-search edge case, " +
			"flaky transition) so future sessions can read it. Records are markdown under " +
			"case-studies/YYYY-MM/<slug>.",
		InputSchema: addCaseStudyInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(addCaseStudySchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("add_case_study", err), nil
		}
		var args addCaseStudyArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("add_case_study", err), nil
		}

		now := time.UnixMilli(tc.Clock.NowMs()).UTC()
		ym := fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month()))
		key := fmt.Sprintf("case-studies/%s/%s", ym, caseStudySlug(args.Title))

		var body strings.Builder
		fmt.Fprintf(&body, "# %s\n\n", args.Title)
		fmt.Fprintf(&body, "**Trigger:** %s\n\n", args.Trigger)
		fmt.Fprintf(&body, "**Solution:** %s\n\n", args.Solution)
		if args.Example != nil {
			fmt.Fprintf(&body, "**Example:**\n\n```\n%s\n```\n\n", *args.Example)
		}
		fmt.Fprintf(&body, "_Added: %s_\n", now.Format("2006-01-02T15:04:05.000Z"))

		if err := tc.Storage.Save(key, body.String()); err != nil {
			return errorResult(err), nil
		}
		tc.Logger.Info("case-study.added", logging.LogContext{"key": key})
		return jsonToolResult(map[string]any{"ok": true, "key": key})
	})
}

var getCaseStudiesInputSchema = jsObject(map[string]any{
	"month":  jsString(),
	"limit":  jsNumber(),
	"offset": jsNumber(),
})

var getCaseStudiesSchema = compileToolSchema("get_case_studies", getCaseStudiesInputSchema)

type getCaseStudiesArgs struct {
	Month  *string `json:"month,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
	Offset *int    `json:"offset,omitempty"`
}

type caseStudyWire struct {
	Key  string `json:"key"`
	Body string `json:"body"`
}

type getCaseStudiesResult struct {
	Count         int             `json:"count"`
	TotalMatching int             `json:"totalMatching"`
	Offset        int             `json:"offset"`
	NextOffset    *int            `json:"nextOffset"`
	Studies       []caseStudyWire `json:"studies"`
}

// registerGetCaseStudiesTool adds the get_case_studies tool: reads
// case-study markdown bodies sorted descending by key.
func registerGetCaseStudiesTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "get_case_studies",
		Description: "List + read case-studies from the playbook. Returns markdown bodies " +
			"sorted descending by key (newest month first). Call at the start of " +
			"a session to surface lessons from previous runs. Supports offset/" +
			"limit pagination plus an optional `month` filter.",
		InputSchema: getCaseStudiesInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(getCaseStudiesSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("get_case_studies", err), nil
		}
		var args getCaseStudiesArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("get_case_studies", err), nil
		}
		limit := 20
		if args.Limit != nil {
			limit = *args.Limit
		}
		offset := 0
		if args.Offset != nil {
			offset = *args.Offset
		}
		prefix := "case-studies"
		if args.Month != nil {
			prefix = "case-studies/" + *args.Month
		}

		keys, err := tc.Storage.List(prefix)
		if err != nil {
			return errorResult(err), nil
		}
		sorted := append([]string(nil), keys...)
		sort.Sort(sort.Reverse(sort.StringSlice(sorted)))

		page := paginate(sorted, offset, limit)
		studies := make([]caseStudyWire, 0, len(page))
		for _, key := range page {
			var body string
			ok, err := tc.Storage.Load(key, &body)
			if err != nil {
				return errorResult(err), nil
			}
			if ok {
				studies = append(studies, caseStudyWire{Key: key, Body: body})
			}
		}
		return jsonToolResult(getCaseStudiesResult{
			Count:         len(studies),
			TotalMatching: len(sorted),
			Offset:        offset,
			NextOffset:    nextOffset(offset, len(studies), len(sorted)),
			Studies:       studies,
		})
	})
}
