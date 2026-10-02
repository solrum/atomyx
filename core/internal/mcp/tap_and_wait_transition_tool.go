package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/orchestra"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultTapAndWaitTimeoutMs    = 10_000
	defaultTapAndWaitMaxTimeoutMs = 60_000
	defaultTapAndWaitIntervalMs   = 300
	// tapAndWaitAppearedFieldLimit bounds the timeout result's
	// "appeared" field to a fixed maximum entry count, so a screen
	// with many new elements does not blow up the response.
	tapAndWaitAppearedFieldLimit = 10
)

var tapAndWaitTransitionInputSchema = jsObject(map[string]any{
	"selector":        selectorInputSchema(),
	"waitForAbsent":   selectorInputSchema(),
	"waitForAppear":   selectorInputSchema(),
	"timeoutMs":       jsNumber(),
	"maxTimeoutMs":    jsNumber(),
	"intervalMs":      jsNumber(),
	"loadingKeywords": jsArray(jsString()),
}, "selector")

var tapAndWaitTransitionSchema = compileToolSchema("tap_and_wait_transition", tapAndWaitTransitionInputSchema)

type tapAndWaitTransitionArgs struct {
	Selector        orchestra.Selector  `json:"selector"`
	WaitForAbsent   *orchestra.Selector `json:"waitForAbsent,omitempty"`
	WaitForAppear   *orchestra.Selector `json:"waitForAppear,omitempty"`
	TimeoutMs       *int                `json:"timeoutMs,omitempty"`
	MaxTimeoutMs    *int                `json:"maxTimeoutMs,omitempty"`
	IntervalMs      *int                `json:"intervalMs,omitempty"`
	LoadingKeywords []string            `json:"loadingKeywords,omitempty"`
}

// registerTapAndWaitTransitionTool adds the tap_and_wait_transition
// tool: an atomic tap plus screen-transition verification. On
// timeout it classifies WHY the expected transition never resolved
// (still_loading, still_loading_with_cancel, dialog_or_error_shown,
// overlay_unknown, partial_transition, no_change_detected) using the
// orchestra transition-diagnostics functions, so the caller gets an
// actionable diagnosis instead of a bare timeout.
func registerTapAndWaitTransitionTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "tap_and_wait_transition",
		Description: "Tap + verify screen transition atomically. Use this INSTEAD of plain tap when " +
			"the tap causes navigation / submit / login. Pass waitForAbsent (old anchor) " +
			"and/or waitForAppear (new anchor). On timeout, returns a structured " +
			"classification (still_loading, dialog_or_error_shown, partial_transition, " +
			"no_change_detected) with an actionable hint. After this succeeds, call " +
			"get_ui_tree to inspect the new screen. Do NOT use screenshot.",
		InputSchema: tapAndWaitTransitionInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, errResult := decodeTapAndWaitTransitionArgs(req.Params.Arguments)
		if errResult != nil {
			return errResult, nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}

		beforeTreeRaw, err := orch.Hierarchy(ctx)
		if err != nil {
			return errorResult(err), nil
		}
		beforeCompact := orchestra.TreeNodeToCompactElements(beforeTreeRaw)
		pre, err := orch.ResolveElementState(ctx, args.Selector)
		if err != nil {
			return errorResult(err), nil
		}

		tapResult, err := orch.Tap(ctx, args.Selector, orchestra.TapOptions{})
		if err != nil {
			return errorResult(err), nil
		}
		if !tapResult.Ok {
			return jsonToolResult(map[string]any{
				"ok":             false,
				"reason":         fmt.Sprintf("tap failed: %s", tapResult.Reason),
				"classification": "tap_failed",
				"hint":           "The tap itself did not succeed. Fix the selector / obscurement issue before retrying.",
				"tapResult":      tapResult,
			})
		}

		timeoutMs := defaultTapAndWaitTimeoutMs
		if args.TimeoutMs != nil {
			timeoutMs = *args.TimeoutMs
		}
		maxTimeoutMs := defaultTapAndWaitMaxTimeoutMs
		if args.MaxTimeoutMs != nil {
			maxTimeoutMs = *args.MaxTimeoutMs
		}
		intervalMs := defaultTapAndWaitIntervalMs
		if args.IntervalMs != nil {
			intervalMs = *args.IntervalMs
		}

		result, err := orch.WaitForTransition(ctx, orchestra.WaitForTransitionParams{
			WaitForAbsent:   args.WaitForAbsent,
			WaitForAppear:   args.WaitForAppear,
			TimeoutMs:       timeoutMs,
			MaxTimeoutMs:    maxTimeoutMs,
			IntervalMs:      intervalMs,
			LoadingKeywords: args.LoadingKeywords,
			Selector:        args.Selector,
			TargetPre:       pre,
			ResolvedBy:      tapResult.ResolvedBy,
			BeforeSnapshot:  beforeCompact,
		})
		if err != nil {
			return errorResult(err), nil
		}

		if result.Ok {
			return jsonToolResult(map[string]any{
				"ok":         true,
				"waitedMs":   result.WaitedMs,
				"resolvedBy": result.ResolvedBy,
			})
		}

		payload := map[string]any{
			"ok":                 false,
			"waitedMs":           result.WaitedMs,
			"classification":     result.Classification,
			"hint":               result.Hint,
			"dialogLabels":       tapAndWaitDialogLabels(result.DialogAppeared),
			"targetStateChanged": result.TargetStateChangeReason,
			"appeared":           tapAndWaitAppearedPayload(result.Appeared),
			"currentSnapshot":    result.CurrentSnapshot,
		}
		if result.Overlay != nil {
			payload["overlayKind"] = result.Overlay.Kind
		}
		return jsonToolResult(payload)
	})
}

func tapAndWaitDialogLabels(dialogAppeared []orchestra.AppearedElement) []string {
	out := make([]string, 0, len(dialogAppeared))
	for _, d := range dialogAppeared {
		out = append(out, d.Label)
	}
	return out
}

func tapAndWaitAppearedPayload(appeared []orchestra.AppearedElement) []map[string]any {
	n := len(appeared)
	if n > tapAndWaitAppearedFieldLimit {
		n = tapAndWaitAppearedFieldLimit
	}
	out := make([]map[string]any, 0, n)
	for _, a := range appeared[:n] {
		sel := a.Selector
		if sel == nil {
			sel = map[string]string{}
		}
		entry := map[string]any{
			"label":           a.Label,
			"role":            a.Role,
			"selector":        sel,
			"looksLikeDialog": a.LooksLikeDialog,
		}
		if a.Secure {
			entry[secureField] = true
		}
		out = append(out, entry)
	}
	return out
}

func decodeTapAndWaitTransitionArgs(raw json.RawMessage) (tapAndWaitTransitionArgs, *mcp.CallToolResult) {
	if err := validateToolArguments(tapAndWaitTransitionSchema, raw); err != nil {
		return tapAndWaitTransitionArgs{}, invalidArgumentsResult("tap_and_wait_transition", err)
	}
	var args tapAndWaitTransitionArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return tapAndWaitTransitionArgs{}, invalidArgumentsResult("tap_and_wait_transition", err)
	}
	if args.WaitForAbsent == nil && args.WaitForAppear == nil {
		return tapAndWaitTransitionArgs{}, invalidArgumentsResult(
			"tap_and_wait_transition",
			fmt.Errorf("must provide waitForAbsent, waitForAppear, or both"),
		)
	}
	return args, nil
}
