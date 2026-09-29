package sidecar

import "dev.atomyx/internal/runner"

// runScriptResultWire is the runScript wire result: the generated run
// id alongside the completed run's outcome.
type runScriptResultWire struct {
	RunID  string           `json:"runId"`
	Result scriptResultWire `json:"result"`
}

// scriptResultWire is the wire shape a script result serializes to.
// Field names and presence must stay stable since a parent process
// decodes them directly. Artifacts marshals as an empty object: a
// captured screenshot is binary data with no JSON representation, so
// the field exists only to keep the "artifacts" key present in the
// decoded shape rather than to carry any data itself.
type scriptResultWire struct {
	ScriptName   string           `json:"scriptName"`
	Ok           bool             `json:"ok"`
	Steps        []stepResultWire `json:"steps"`
	TotalSteps   int              `json:"totalSteps"`
	PassedSteps  int              `json:"passedSteps"`
	FailedAtStep *int             `json:"failedAtStep,omitempty"`
	DurationMs   int64            `json:"durationMs"`
	Artifacts    struct{}         `json:"artifacts"`
	// Abort is present only when the run stopped early for a reason
	// beyond an ordinary step failure, such as the device or agent
	// going unreachable. Its absence is not itself meaningful to a
	// decoder: an existing consumer that has never read this field
	// keeps working unchanged.
	Abort *runAbortWire `json:"abort,omitempty"`
}

// runAbortWire is the wire shape of a runner.RunAbort: why a run
// stopped early, alongside the diagnostic code, message, and remedy a
// caller renders for it.
type runAbortWire struct {
	Reason  string `json:"reason"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Remedy  string `json:"remedy,omitempty"`
}

type stepResultWire struct {
	StepIndex  int     `json:"stepIndex"`
	Command    string  `json:"command"`
	Ok         bool    `json:"ok"`
	Detail     *string `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
}

func toScriptResultWire(r runner.ScriptResult) scriptResultWire {
	steps := make([]stepResultWire, len(r.Steps))
	for i, s := range r.Steps {
		steps[i] = stepResultWire{
			StepIndex:  s.StepIndex,
			Command:    s.Command,
			Ok:         s.Ok,
			Detail:     s.Detail,
			DurationMs: s.DurationMs,
		}
	}
	var abort *runAbortWire
	if r.Abort != nil {
		abort = &runAbortWire{
			Reason:  string(r.Abort.Reason),
			Code:    r.Abort.Code,
			Message: r.Abort.Message,
			Remedy:  r.Abort.Remedy,
		}
	}
	return scriptResultWire{
		ScriptName:   r.ScriptName,
		Ok:           r.Ok,
		Steps:        steps,
		TotalSteps:   r.TotalSteps,
		PassedSteps:  r.PassedSteps,
		FailedAtStep: r.FailedAtStep,
		DurationMs:   r.DurationMs,
		Abort:        abort,
	}
}

type isRunningResult struct {
	Running bool `json:"running"`
}

type stepTokenWire struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// stepEventWire is one runStarted/stepStarted/stepCompleted/... step
// notification, flattening every step-event variant into one struct
// alongside its runId. Ok, Detail, and DurationMs are marshaled only
// when Type is "stepCompleted". Every other event type omits them so
// a decoder never sees a stray zero-valued "ok" or "durationMs" for
// an event that doesn't carry one.
type stepEventWire struct {
	RunID      string          `json:"runId"`
	Type       string          `json:"type"`
	StepIndex  int             `json:"stepIndex"`
	Command    string          `json:"command"`
	Summary    string          `json:"summary"`
	Tokens     []stepTokenWire `json:"tokens"`
	Depth      int             `json:"depth"`
	Line       *int            `json:"line,omitempty"`
	Ok         *bool           `json:"ok,omitempty"`
	Detail     *string         `json:"detail,omitempty"`
	DurationMs *int64          `json:"durationMs,omitempty"`
}

func toStepEventWire(runID string, e runner.StepEvent) stepEventWire {
	tokens := make([]stepTokenWire, len(e.Tokens))
	for i, t := range e.Tokens {
		tokens[i] = stepTokenWire{Kind: string(t.Kind), Text: t.Text}
	}
	w := stepEventWire{
		RunID:     runID,
		Type:      string(e.Type),
		StepIndex: e.StepIndex,
		Command:   e.Command,
		Summary:   e.Summary,
		Tokens:    tokens,
		Depth:     e.Depth,
		Line:      e.Line,
	}
	if e.Type == runner.StepCompleted {
		ok := e.Ok
		durationMs := e.DurationMs
		w.Ok = &ok
		w.Detail = e.Detail
		w.DurationMs = &durationMs
	}
	return w
}

type runStartedPayload struct {
	RunID      string `json:"runId"`
	StartedAt  int64  `json:"startedAt"`
	ScriptName string `json:"scriptName"`
}

type runCompletedPayload struct {
	RunID      string `json:"runId"`
	Ok         bool   `json:"ok"`
	Passed     int    `json:"passed"`
	Total      int    `json:"total"`
	DurationMs int64  `json:"durationMs"`
}

type runErroredPayload struct {
	RunID   string `json:"runId"`
	Message string `json:"message"`
}

type runStopRequestedPayload struct {
	RunID string `json:"runId"`
}
