package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/diag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compileToolSchema compiles schema (a draft-07 JSON Schema, as
// produced by the js* builders in schema_helpers.go) for tool name.
// It panics on a malformed schema: a broken tool schema is a
// programming error, not a runtime condition, so every tool
// registration compiles its schema at startup rather than deferring
// the failure to the first call.
func compileToolSchema(name string, schema map[string]any) *jsonschema.Schema {
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("mcp: marshaling %s input schema: %w", name, err))
	}
	compiler := jsonschema.NewCompiler()
	resourceName := name + "-input.json"
	if err := compiler.AddResource(resourceName, bytes.NewReader(raw)); err != nil {
		panic(fmt.Errorf("mcp: adding %s input schema resource: %w", name, err))
	}
	compiled, err := compiler.Compile(resourceName)
	if err != nil {
		panic(fmt.Errorf("mcp: compiling %s input schema: %w", name, err))
	}
	return compiled
}

// validateToolArguments parses raw as JSON and validates it against
// schema. It returns a non-nil error when raw is not valid JSON or
// fails schema validation; callers convert that into an
// isError:true result via invalidArgumentsResult.
func validateToolArguments(schema *jsonschema.Schema, raw json.RawMessage) error {
	var doc any
	if len(raw) == 0 {
		doc = map[string]any{}
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	return schema.Validate(doc)
}

// invalidArgumentsResult builds the isError:true result returned when
// a tool call's arguments fail schema or value-constraint validation.
// The message names the failing tool so a caller can tell which call
// an error belongs to when several tool calls are in flight at once.
// It carries diag.CodeInvalidArgument, the same code an on-device
// agent's own caller-input rejection surfaces (see
// diag.ClassifyAgentBoundary), so a caller can branch on one code
// regardless of which layer caught the bad argument.
func invalidArgumentsResult(toolName string, err error) *mcp.CallToolResult {
	message := fmt.Sprintf("Invalid arguments for %s: %v", toolName, err)
	return errorResult(diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, err))
}

// jsonToolResult marshals v as JSON indented two spaces per level and
// wraps it as a successful (isError:false) text-content tool result.
// The indentation is part of the tool surface's contract: a client
// may rely on the exact formatting of a successful result's text.
func jsonToolResult(v any) (*mcp.CallToolResult, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mcp: marshaling tool result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil
}

// codedErrorPayload is the structured isError:true payload errorResult
// emits for every tool failure that is not a per-call timeout (see
// toolTimeoutPayload for that structured shape): a stable code, the
// failing operation's own message, and an actionable hint when the
// failure carries one.
type codedErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// errorResult builds an isError:true tool result describing err. It
// resolves err's stable code and remedy via diag.Describe, so every
// tool failure this surface reports carries the same structured
// shape a caller can branch on instead of matching message text.
func errorResult(err error) *mcp.CallToolResult {
	code, message, remedy := diag.Describe(err)
	return marshalStructuredResult(codedErrorPayload{Code: code, Message: message, Hint: remedy})
}
