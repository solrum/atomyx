package orchestra

// ObscurerInfo identifies the element that visually blocks a target
// element's tap point, as reported by DetectObscurement.
type ObscurerInfo struct {
	Role  string `json:"role"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ActionResult is the outcome of a high-level Orchestra action.
// Every mutating Orchestra method returns this shape so a caller can
// read a consistent outcome without branching on method name.
//
// Ok true means the action was dispatched; ResolvedBy and Detail are
// then populated when the action has something to report and are
// empty otherwise. Ok false means the action was not dispatched
// because Orchestra's own pipeline (selector resolution, scroll-into-
// view, obscurement) rejected it; Reason explains why, and Obscurer
// carries the blocking element's identity when the rejection was an
// obscurement.
//
// Reason is safe to show to a downstream AI agent: it describes WHAT
// failed and, where obvious, suggests a recovery. It is not a
// machine-readable error code; a caller wanting programmatic error
// handling should check the typed errors FindTimeoutError and
// ScrollUnreachableError directly, since Orchestra only converts
// those into an ActionResult at the action-method boundary.
type ActionResult struct {
	Ok         bool          `json:"ok"`
	ResolvedBy string        `json:"resolvedBy,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	Reason     string        `json:"reason,omitempty"`
	Obscurer   *ObscurerInfo `json:"obscurer,omitempty"`
}

// ActionOk builds a successful ActionResult. Pass "" for resolvedBy
// or detail when the caller has nothing to report for that field.
func ActionOk(resolvedBy, detail string) ActionResult {
	return ActionResult{Ok: true, ResolvedBy: resolvedBy, Detail: detail}
}

// ActionFail builds a failed ActionResult. obscurer is nil when the
// failure was not caused by a detected overlay.
func ActionFail(reason string, obscurer *ObscurerInfo) ActionResult {
	return ActionResult{Ok: false, Reason: reason, Obscurer: obscurer}
}
