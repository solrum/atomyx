package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolCallMethod is the MCP wire method name for a tool invocation.
// newToolCallMiddleware compares against this literal because
// AddReceivingMiddleware wraps every method the server receives
// (initialize, tools/list, prompts/get, ...), not just tool calls.
const toolCallMethod = "tools/call"

const (
	// defaultToolTimeoutMs is the per-call budget granted when neither
	// the call's own arguments nor ATOMYX_TOOL_TIMEOUT_MS declare one.
	defaultToolTimeoutMs = 30_000
	// toolTimeoutBufferMs is added on top of a call's own declared
	// timeoutMs/maxTimeoutMs. The buffer gives a tool's own internal
	// deadline check (e.g. wait_for_element's poll loop) a chance to
	// return its own structured result before this wrapper's hard
	// kill fires; the wrapper is the safety net for the case where
	// the underlying device call never honors that internal deadline
	// (a transport hang).
	toolTimeoutBufferMs = 5_000
	// toolTimeoutEnvVar overrides the default per-call budget for
	// calls that declare no timeoutMs/maxTimeoutMs of their own.
	toolTimeoutEnvVar = "ATOMYX_TOOL_TIMEOUT_MS"
)

// toolTimeoutHint is the exact guidance text returned to a client when
// a tool call times out. Its wording is part of the tool surface's
// contract and must not drift between releases.
const toolTimeoutHint = "Tool exceeded its time budget and the underlying device " +
	"call was aborted. The device transport may be hung. " +
	"Recommended next steps: (1) call get_ui_tree to re-orient " +
	"(its own short poll budget will fail fast if the driver " +
	"is dead), (2) if get_ui_tree also times out, call " +
	"select_device again to reconnect, (3) if a specific tool " +
	"consistently times out on this device, abort the run and " +
	"report the device id."

// toolTimeoutPayload is the structured isError body returned when the
// wrapper's own deadline fires before the tool handler returns. Field
// order is part of this tool surface's stable contract and must not
// change between releases.
type toolTimeoutPayload struct {
	Code             string `json:"code"`
	Tool             string `json:"tool"`
	ElapsedMs        int64  `json:"elapsedMs"`
	BudgetMs         int64  `json:"budgetMs"`
	DeclaredBudgetMs *int64 `json:"declaredBudgetMs"`
	Hint             string `json:"hint"`
}

// toolTimeoutResult marshals payload as the sole text content of an
// isError:true tool result.
func toolTimeoutResult(payload toolTimeoutPayload) *mcp.CallToolResult {
	return marshalStructuredResult(payload)
}

// marshalStructuredResult marshals payload as the sole text content of
// an isError:true tool result. It is the one encoding path every
// structured failure this tool surface emits goes through, whether
// payload is a toolTimeoutPayload or a codedErrorPayload, so there is
// exactly one isError:true shape produced rather than one per failure
// kind.
func marshalStructuredResult(payload any) *mcp.CallToolResult {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// Every payload this function is called with is built from
		// static strings and numbers already on hand; a marshal
		// failure here can only be a bug in this file.
		panic(fmt.Errorf("mcp: marshaling structured error payload: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		IsError: true,
	}
}

// declaredTimeoutMs reads a number-typed timeoutMs/maxTimeoutMs field
// out of a tool call's raw arguments. Any other JSON type for the
// field is treated as absent, because checking the field's type is
// each tool's own structural-validation concern, not the wrapper's.
func declaredTimeoutMs(rawArgs json.RawMessage) *float64 {
	if len(rawArgs) == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawArgs, &decoded); err != nil {
		return nil
	}
	for _, key := range [...]string{"maxTimeoutMs", "timeoutMs"} {
		v, ok := decoded[key]
		if !ok {
			continue
		}
		if n, ok := v.(float64); ok {
			return &n
		}
	}
	return nil
}

// envDefaultToolTimeoutMs reads ATOMYX_TOOL_TIMEOUT_MS as the default
// per-call budget in milliseconds. An unset variable, a value that
// fails to parse as a number, or a value that parses to exactly zero
// all fall back to defaultToolTimeoutMs. Any other numeric value,
// including a negative one, is returned verbatim.
func envDefaultToolTimeoutMs() float64 {
	if raw, ok := os.LookupEnv(toolTimeoutEnvVar); ok {
		if n, err := strconv.ParseFloat(raw, 64); err == nil && n != 0 {
			return n
		}
	}
	return defaultToolTimeoutMs
}

// resolveToolTimeoutBudget computes the wrapper's per-call deadline:
// the call's own declared budget plus a fixed buffer, floored at the
// server's default budget, so a declared budget smaller than the
// default can never shrink the wrapper's deadline below what an
// undeclared call would get.
func resolveToolTimeoutBudget(rawArgs json.RawMessage) (budgetMs float64, declaredBudgetMs *float64) {
	declared := declaredTimeoutMs(rawArgs)
	defaultMs := envDefaultToolTimeoutMs()
	if declared == nil {
		return defaultMs, nil
	}
	budget := *declared + toolTimeoutBufferMs
	if defaultMs > budget {
		budget = defaultMs
	}
	return budget, declared
}

// newToolCallMiddleware builds the single choke point every tools/call
// request passes through, applying three behaviors uniformly to every
// registered tool without touching each tool's own handler:
//
//  1. Value-constraint validation the published (deliberately loose)
//     input schemas don't enforce. See value_constraint_schemas.go.
//  2. A hard per-call timeout, enforced through the call's context. The
//     timeout only aborts a call whose downstream code observes
//     ctx.Done() (as the Driver port's calls do); a tool that ignores
//     it blocks the wrapper right along with it, because the wrapper's
//     timeout depends entirely on the underlying call observing it in
//     order to actually stop. Acquiring a device is exempt: the code
//     that acquires one pauses the budget, which starts over in full
//     once the device is ready.
//  3. The session bookkeeping that bounds how long a device stays held:
//     the call is reported to sess when it starts and when it ends, and
//     a call on a run the open-run idle timeout closed is refused with
//     RUN_EXPIRED.
func newToolCallMiddleware(logger logging.Logger, clk clock.Clock, sess *DeviceSession) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != toolCallMethod {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return next(ctx, method, req)
			}

			if verr := validateToolValueConstraints(params.Name, params.Arguments); verr != nil {
				return invalidArgumentsResult(params.Name, verr), nil
			}

			if err := sess.BeginCall(params.Name); err != nil {
				return errorResult(err), nil
			}
			defer sess.EndCall() //nolint:contextcheck // idle timers outlive any call, so they take no call context

			budgetMs, declaredBudgetMs := resolveToolTimeoutBudget(params.Arguments)
			callCtx, stop := newBudgetContext(ctx, clk, int64(budgetMs))
			defer stop()

			startedAt := clk.NowMs()
			result, err := next(withProgress(callCtx, req, params), method, req)
			if !errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				return result, err
			}

			elapsedMs := clk.NowMs() - startedAt
			var declaredOut *int64
			if declaredBudgetMs != nil {
				d := int64(*declaredBudgetMs)
				declaredOut = &d
			}
			if logger != nil {
				logger.Warn("tool.timeout", logging.LogContext{
					"tool":       params.Name,
					"durationMs": elapsedMs,
					"budgetMs":   int64(budgetMs),
				})
			}
			return toolTimeoutResult(toolTimeoutPayload{
				Code:             diag.CodeToolTimeout,
				Tool:             params.Name,
				ElapsedMs:        elapsedMs,
				BudgetMs:         int64(budgetMs),
				DeclaredBudgetMs: declaredOut,
				Hint:             toolTimeoutHint,
			}), nil
		}
	}
}
