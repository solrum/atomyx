package sidecar

import (
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/scenario"
)

// runScenarioResultWire is the runScript wire result for a
// scenario-shaped script: the generated run id alongside the
// completed scenario's outcome.
type runScenarioResultWire struct {
	RunID  string             `json:"runId"`
	Result scenarioResultWire `json:"result"`
}

// scenarioResultWire is the wire shape a scenario result serializes
// to. Field names and presence must stay stable since a parent
// process decodes them directly.
type scenarioResultWire struct {
	ScenarioName  string                     `json:"scenarioName"`
	Ok            bool                       `json:"ok"`
	Scripts       []scenarioScriptResultWire `json:"scripts"`
	TotalScripts  int                        `json:"totalScripts"`
	PassedScripts int                        `json:"passedScripts"`
	DurationMs    int64                      `json:"durationMs"`
}

// scenarioScriptResultWire is one child script's outcome in the
// sidecar's wire contract. Result nests the same scriptResultWire
// mapping runScript's own wire result uses, so a parent process that
// already decodes runScript's output can decode a passed or failed
// child script's fields the identical way.
type scenarioScriptResultWire struct {
	ScriptIndex int               `json:"scriptIndex"`
	ScriptPath  string            `json:"scriptPath"`
	Status      string            `json:"status"`
	Result      *scriptResultWire `json:"result,omitempty"`
	Error       *string           `json:"error,omitempty"`
	DurationMs  int64             `json:"durationMs"`
}

func toScenarioResultWire(r scenario.ScenarioResult) scenarioResultWire {
	scripts := make([]scenarioScriptResultWire, len(r.Scripts))
	for i, s := range r.Scripts {
		var result *scriptResultWire
		if s.Result != nil {
			w := toScriptResultWire(*s.Result)
			result = &w
		}
		scripts[i] = scenarioScriptResultWire{
			ScriptIndex: s.ScriptIndex,
			ScriptPath:  s.ScriptPath,
			Status:      string(s.Status),
			Result:      result,
			Error:       s.Error,
			DurationMs:  s.DurationMs,
		}
	}
	return scenarioResultWire{
		ScenarioName:  r.ScenarioName,
		Ok:            r.Ok,
		Scripts:       scripts,
		TotalScripts:  r.TotalScripts,
		PassedScripts: r.PassedScripts,
		DurationMs:    r.DurationMs,
	}
}

// runStartedScenarioPayload is the runStarted event payload for a
// scenario run: fields the sidecar's wire contract fixes for a
// parent process to decode when a scenario run begins.
type runStartedScenarioPayload struct {
	RunID        string `json:"runId"`
	StartedAt    int64  `json:"startedAt"`
	ScenarioName string `json:"scenarioName"`
	TotalScripts int    `json:"totalScripts"`
}

// runCompletedScenarioPayload is the runCompleted event payload for a
// scenario run: fields the sidecar's wire contract fixes for a
// parent process to decode when a scenario run finishes.
type runCompletedScenarioPayload struct {
	RunID         string `json:"runId"`
	Ok            bool   `json:"ok"`
	PassedScripts int    `json:"passedScripts"`
	TotalScripts  int    `json:"totalScripts"`
	DurationMs    int64  `json:"durationMs"`
}

// scenarioEventWire is one scenarioStarted/scriptStarted/
// scriptCompleted/scenarioCompleted notification, flattening every
// variant's fields into one struct alongside its runId. Its
// MarshalJSON emits only the fields that variant actually carries, so
// (for example) a scenarioStarted event never gains a stray
// "scriptIndex": 0 key a decoder might mistake for real data.
type scenarioEventWire struct {
	RunID         string
	Type          scenario.ScenarioEventType
	ScenarioName  string
	TotalScripts  int
	ScriptIndex   int
	ScriptPath    string
	Status        string
	DurationMs    int64
	FailedAtStep  *int
	Ok            bool
	PassedScripts int
}

func toScenarioEventWire(runID string, e scenario.ScenarioEvent) scenarioEventWire {
	return scenarioEventWire{
		RunID:         runID,
		Type:          e.Type,
		ScenarioName:  e.ScenarioName,
		TotalScripts:  e.TotalScripts,
		ScriptIndex:   e.ScriptIndex,
		ScriptPath:    e.ScriptPath,
		Status:        string(e.Status),
		DurationMs:    e.DurationMs,
		FailedAtStep:  e.FailedAtStep,
		Ok:            e.Ok,
		PassedScripts: e.PassedScripts,
	}
}

func (w scenarioEventWire) MarshalJSON() ([]byte, error) {
	switch w.Type {
	case scenario.ScenarioStarted:
		return json.Marshal(struct {
			RunID        string `json:"runId"`
			Type         string `json:"type"`
			ScenarioName string `json:"scenarioName"`
			TotalScripts int    `json:"totalScripts"`
		}{w.RunID, string(w.Type), w.ScenarioName, w.TotalScripts})
	case scenario.ScriptStarted:
		return json.Marshal(struct {
			RunID       string `json:"runId"`
			Type        string `json:"type"`
			ScriptIndex int    `json:"scriptIndex"`
			ScriptPath  string `json:"scriptPath"`
		}{w.RunID, string(w.Type), w.ScriptIndex, w.ScriptPath})
	case scenario.ScriptCompleted:
		return json.Marshal(struct {
			RunID        string `json:"runId"`
			Type         string `json:"type"`
			ScriptIndex  int    `json:"scriptIndex"`
			ScriptPath   string `json:"scriptPath"`
			Status       string `json:"status"`
			DurationMs   int64  `json:"durationMs"`
			FailedAtStep *int   `json:"failedAtStep,omitempty"`
		}{w.RunID, string(w.Type), w.ScriptIndex, w.ScriptPath, w.Status, w.DurationMs, w.FailedAtStep})
	case scenario.ScenarioCompleted:
		return json.Marshal(struct {
			RunID         string `json:"runId"`
			Type          string `json:"type"`
			Ok            bool   `json:"ok"`
			TotalScripts  int    `json:"totalScripts"`
			PassedScripts int    `json:"passedScripts"`
			DurationMs    int64  `json:"durationMs"`
		}{w.RunID, string(w.Type), w.Ok, w.TotalScripts, w.PassedScripts, w.DurationMs})
	default:
		return nil, fmt.Errorf("scenarioEventWire: unknown event type %q", w.Type)
	}
}
