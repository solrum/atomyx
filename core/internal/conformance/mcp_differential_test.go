package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	atomcp "dev.atomyx/internal/mcp"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpToolSchemaGoldenRelPath is the golden fixture
// TestMcpToolSchemaDifferential checks the Go MCP server's tool
// schema map against.
var mcpToolSchemaGoldenRelPath = filepath.Join("mcp", "tool-schema.json")

const mcpConformanceTimeout = 15 * time.Second

// mcpConformanceEpoch is the instant the server under test sees as
// "now". add_case_study derives its "case-studies/<year>-<month>"
// storage key from the server clock, and the golden records that key,
// so the test must pin the clock instead of reading the host's wall
// time, or the golden goes stale at every month boundary.
var mcpConformanceEpoch = time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)

// newGoMcpClientSession wires an in-process Go MCP server against a
// mcpScriptedMockFactory seeded with program and connects a client
// session to it over an in-memory transport.
func newGoMcpClientSession(t *testing.T, ctx context.Context, program *McpDriverProgram) (*mcp.ClientSession, func()) {
	t.Helper()
	root := t.TempDir()
	store := storage.NewInMemoryStorage()
	runStore := storage.NewRunStore()
	server, err := atomcp.NewServer(atomcp.Info{Name: "atomyx-conformance", Version: "0.0.0-test"}, atomcp.ServerDeps{
		Probe: device.NewStaticProbe([]device.Descriptor{
			{ID: "mock-device-2", Platform: device.PlatformIOS, Kind: device.KindSimulator},
		}),
		Factory:  &mcpScriptedMockFactory{program: program},
		Storage:  store,
		RunStore: runStore,
		Runs:     storage.NewRunCatalog(store, root, runfolder.NewLockLiveness(root)),
		RunStarter: runfolder.NewStarter(root,
			func() time.Time { return mcpConformanceEpoch }, &mcpSuffixReader{}),
		RunRoot: root,
		Clock:   orchestra.NewFakeClock(mcpConformanceEpoch.UnixMilli()),
	})
	if err != nil {
		t.Fatalf("Go mcp NewServer: %v", err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("Go mcp server.Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "atomyx-conformance-client", Version: "0.0.0-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Go mcp client.Connect: %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

// mcpSuffixReader yields an increasing byte sequence, so the run ids
// the server mints are the same on every replay.
type mcpSuffixReader struct{ n byte }

func (r *mcpSuffixReader) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

// mcpToolSchema is the tools/list shape this differential compares:
// description and JSON Schema only, keyed by tool name so the two
// engines' registration order (which need not match) never surfaces
// as a diff.
type mcpToolSchema struct {
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// goOnlyMcpTools lists tool names the Go MCP server registers that
// the frozen golden's tool list does not carry, because the reference
// implementation never had a matching tool. This differential's job is
// to keep every shared tool byte-identical to the golden; a tool
// deliberately unique to Go is out of scope for it, so this set is
// excluded from the comparison rather than causing a perpetual,
// expected mismatch.
var goOnlyMcpTools = map[string]bool{
	"check_health": true,
}

func mcpToolSchemaMap(tools []*mcp.Tool) map[string]mcpToolSchema {
	out := make(map[string]mcpToolSchema, len(tools))
	for _, tool := range tools {
		if goOnlyMcpTools[tool.Name] {
			continue
		}
		out[tool.Name] = mcpToolSchema{Description: tool.Description, InputSchema: tool.InputSchema}
	}
	return out
}

// TestMcpToolSchemaDifferential diffs the Go MCP server's tools/list
// response against a frozen golden fixture of the shared tool
// schemas.
func TestMcpToolSchemaDifferential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpConformanceTimeout)
	defer cancel()

	goSession, closeGo := newGoMcpClientSession(t, ctx, nil)
	defer closeGo()

	goTools, err := goSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("Go ListTools: %v", err)
	}

	goJSON, err := json.Marshal(mcpToolSchemaMap(goTools.Tools))
	if err != nil {
		t.Fatalf("marshaling Go tool schemas: %v", err)
	}

	checkGolden(t, mcpToolSchemaGoldenRelPath, goJSON, DiffJSON)
}

// mcpStepOutcome is one step's live, normalized outcome.
type mcpStepOutcome struct {
	Tool   string `json:"tool"`
	Result any    `json:"result"`
}

// mcpCallContext carries the ids a case's earlier steps produced, so
// a later step can reference "the run start_run just created" or
// "the bug report_bug just created" without the corpus hard-coding a
// value the Go server mints unpredictably at replay time.
type mcpCallContext struct {
	lastRunID string
	lastBugID string
}

// resolveMcpArgPlaceholders substitutes the literal string values
// "$lastRunId" and "$lastBugId" appearing anywhere in args with the
// ids sc has captured so far, so a corpus step can address a record a
// previous step in the same case created.
func resolveMcpArgPlaceholders(args json.RawMessage, sc *mcpCallContext) json.RawMessage {
	if len(args) == 0 {
		return args
	}
	replaced := strings.ReplaceAll(string(args), `"$lastRunId"`, strconv.Quote(sc.lastRunID))
	replaced = strings.ReplaceAll(replaced, `"$lastBugId"`, strconv.Quote(sc.lastBugID))
	return json.RawMessage(replaced)
}

func decodeMcpArgs(args json.RawMessage) (map[string]any, error) {
	if len(args) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// decodeMcpCallResult turns a CallToolResult into a plain JSON value:
// {isError, content: [...]}, where each text content item is decoded
// from its JSON string when it parses as JSON (every tool result in
// this surface is JSON.stringify'd application data), and kept as a
// raw string otherwise (an unstructured error message).
func decodeMcpCallResult(result *mcp.CallToolResult) (map[string]any, error) {
	content := make([]any, 0, len(result.Content))
	for _, item := range result.Content {
		text, ok := item.(*mcp.TextContent)
		if !ok {
			return nil, fmt.Errorf("unsupported content type %T", item)
		}
		var decoded any
		if err := json.Unmarshal([]byte(text.Text), &decoded); err == nil {
			content = append(content, decoded)
		} else {
			content = append(content, text.Text)
		}
	}
	return map[string]any{"isError": result.IsError, "content": content}, nil
}

// captureMcpIDs records the ids resolveMcpArgPlaceholders needs from
// a step's own (un-normalized) result, before normalization replaces
// them with a placeholder.
func captureMcpIDs(tool string, raw map[string]any, sc *mcpCallContext) {
	content, ok := raw["content"].([]any)
	if !ok || len(content) == 0 {
		return
	}
	body, ok := content[0].(map[string]any)
	if !ok {
		return
	}
	switch tool {
	case "start_run":
		if id, ok := body["id"].(string); ok {
			sc.lastRunID = id
		}
	case "report_bug":
		if id, ok := body["id"].(string); ok {
			sc.lastBugID = id
		}
	}
}

// mcpNondeterministicFieldsByTool names, per tool, the wire fields
// whose value is inherently non-deterministic. A run or bug id embeds
// RunStore's own wall-clock timestamp: the storage this test composes
// the server against runs RunStore on the real SystemClock, unlike
// Orchestra's own polling clock, which this test pins to a fake, so a
// run or bug id can never be pinned to a fixed golden value.
// startedAt, finishedAt, durationMs, timestamp, connectedAt, waitedMs,
// and path are wall-clock derived for the same reason. Normalization
// replaces the value only; a field the golden carries that Go's
// response omits, or vice versa, still surfaces as a real mismatch.
var mcpNondeterministicFieldsByTool = map[string][]string{
	"select_device":           {"connectedAt"},
	"start_run":               {"id", "startedAt", "erroredPredecessor"},
	"finish_run":              {"id", "durationMs", "timestamp"},
	"report_bug":              {"id", "runId"},
	"list_runs":               {"id", "startedAt", "finishedAt", "durationMs", "timestamp"},
	"get_run":                 {"id", "startedAt", "finishedAt", "durationMs", "timestamp", "reason"},
	"update_run_summary":      {"id", "reason"},
	"delete_run":              {"id", "reason"},
	"list_bugs":               {"id", "runId", "timestamp"},
	"get_bug":                 {"id", "runId", "timestamp", "reason"},
	"delete_bug":              {"id", "reason"},
	"screenshot":              {"path"},
	"tap_and_wait_transition": {"waitedMs"},
	"run_script":              {"durationMs"},
}

const mcpNormalizedPlaceholder = "<normalized>"

// normalizeMcpValue replaces every occurrence of a field named in
// fields, at any nesting depth, with a fixed placeholder.
func normalizeMcpValue(v any, fields map[string]bool) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			if fields[k] {
				out[k] = mcpNormalizedPlaceholder
				continue
			}
			out[k] = normalizeMcpValue(vv, fields)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeMcpValue(vv, fields)
		}
		return out
	default:
		return val
	}
}

// caseStudyAddedLinePattern matches add_case_study's trailing
// "_Added: <ISO-8601 with milliseconds>_" line, embedded inside the
// markdown body text rather than carried as its own wire field. The
// timestamp comes from the same wall-clock source as every other
// normalized field, so it gets the same placeholder treatment even
// though it lives inside a string value rather than at a JSON key.
var caseStudyAddedLinePattern = regexp.MustCompile(`_Added: [^_]*_`)

func normalizeCaseStudyBody(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			if k == "body" {
				if s, ok := vv.(string); ok {
					out[k] = caseStudyAddedLinePattern.ReplaceAllString(s, mcpNormalizedPlaceholder)
					continue
				}
			}
			out[k] = normalizeCaseStudyBody(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeCaseStudyBody(vv)
		}
		return out
	default:
		return v
	}
}

func normalizeMcpResult(tool string, v any) any {
	if tool == "add_case_study" || tool == "get_case_studies" {
		v = normalizeCaseStudyBody(v)
	}
	names := mcpNondeterministicFieldsByTool[tool]
	if len(names) == 0 {
		return v
	}
	fields := make(map[string]bool, len(names))
	for _, name := range names {
		fields[name] = true
	}
	return normalizeMcpValue(v, fields)
}

// callMcpTool resolves step's placeholders against sc, calls the
// tool, captures any id the step produced, and returns the
// normalized outcome DiffJSON compares.
func callMcpTool(ctx context.Context, session *mcp.ClientSession, step McpStep, sc *mcpCallContext) (mcpStepOutcome, error) {
	arguments, err := decodeMcpArgs(resolveMcpArgPlaceholders(step.Args, sc))
	if err != nil {
		return mcpStepOutcome{}, fmt.Errorf("decoding args for %s: %w", step.Tool, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: step.Tool, Arguments: arguments})
	if err != nil {
		return mcpStepOutcome{}, fmt.Errorf("calling %s: %w", step.Tool, err)
	}
	raw, err := decodeMcpCallResult(result)
	if err != nil {
		return mcpStepOutcome{}, fmt.Errorf("decoding %s result: %w", step.Tool, err)
	}
	captureMcpIDs(step.Tool, raw, sc)
	return mcpStepOutcome{Tool: step.Tool, Result: normalizeMcpResult(step.Tool, raw)}, nil
}

// replayMcpSteps sends every step to session in order, awaiting each
// response before sending the next.
func replayMcpSteps(ctx context.Context, session *mcp.ClientSession, steps []McpStep) ([]mcpStepOutcome, error) {
	sc := &mcpCallContext{}
	outcomes := make([]mcpStepOutcome, 0, len(steps))
	for _, step := range steps {
		outcome, err := callMcpTool(ctx, session, step, sc)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// TestMcpToolCallDifferential diffs the Go MCP server's live step
// trace over every corpus/mcp case against a frozen golden fixture.
// It excludes the tap_and_wait_transition TIMEOUT path on purpose: the
// Go core reports a reduced-fidelity generic transition
// classification there, a documented, deferred gap (see
// registerTapAndWaitTransitionTool's own doc comment) rather than a
// wire-shape bug. The success path (this corpus's
// tap-and-wait-transition-success.json) and the immediate-tap-failure
// path (tap-and-wait-transition-immediate-failure.json) return the
// shape the golden expects and are covered here.
func TestMcpToolCallDifferential(t *testing.T) {
	cases, err := LoadMcpCorpus("corpus/mcp")
	if err != nil {
		t.Fatalf("loading mcp corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("mcp corpus is empty")
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), mcpConformanceTimeout)
			defer cancel()

			goSession, closeGo := newGoMcpClientSession(t, ctx, c.Driver)
			defer closeGo()

			goTrace, err := replayMcpSteps(ctx, goSession, c.Steps)
			if err != nil {
				t.Fatalf("Go replay: %v", err)
			}

			goJSON, err := json.Marshal(goTrace)
			if err != nil {
				t.Fatalf("marshaling Go trace: %v", err)
			}

			checkGolden(t, filepath.Join("mcp", c.Name+".json"), goJSON, DiffJSON)
		})
	}
}
