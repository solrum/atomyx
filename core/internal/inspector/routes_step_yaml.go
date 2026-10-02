package inspector

import (
	"errors"
	"net/http"
	"strings"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/session"
)

// Actions step/yaml renders. Each acts on one selector.
const (
	actionTap              = "tap"
	actionAssertVisible    = "assertVisible"
	actionAssertNotVisible = "assertNotVisible"
	actionAssertExists     = "assertExists"
	actionWaitFor          = "waitFor"
	actionType             = "type"
)

var supportedActions = []string{
	actionTap, actionAssertVisible, actionAssertNotVisible, actionAssertExists, actionWaitFor, actionType,
}

// securePlaceholderName is the environment variable a rendered type
// step names in place of the text of a secure field. The script reads
// it from its env block or the command line, so the secret never
// appears in the YAML.
const securePlaceholderName = "password"

type stepYAMLRequest struct {
	Selector orchestra.Selector `json:"selector"`
	Action   string             `json:"action"`
	// Text is what the type action types. Other actions refuse it.
	Text *string `json:"text"`
}

type stepYAMLResponse struct {
	YAML string `json:"yaml"`
	// Secure is true when the target is a secure field, so the step
	// holds a placeholder instead of the text.
	Secure bool `json:"secure"`
	// Placeholder is the environment variable the step names, set only
	// when Secure is true.
	Placeholder string `json:"placeholder,omitempty"`
}

// stepYAMLHandler renders one script step for a selector and an
// action. For type, it resolves the selector in the stored tree, and
// when any match is secure it renders the placeholder and never the
// text. When the selector matches nothing in the stored tree the
// handler cannot tell whether the field is secure, so it refuses
// instead of rendering a literal.
func stepYAMLHandler(c session.Viewer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req stepYAMLRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeFailure(w, err)
			return
		}
		if !isSupportedAction(req.Action) {
			writeFailure(w, invalidArgument("the action "+quote(req.Action)+" is not supported",
				"Use one of: "+strings.Join(supportedActions, ", ")+"."))
			return
		}
		if req.Action != actionType && req.Text != nil {
			writeFailure(w, invalidArgument("text applies to the type action only", "Remove text, or use the type action."))
			return
		}
		if err := orchestra.RequireNonEmpty(req.Selector); err != nil {
			writeFailure(w, err)
			return
		}
		sel, err := scriptSelector(req.Selector)
		if err != nil {
			writeFailure(w, err)
			return
		}
		text, secure := "", false
		if req.Action == actionType {
			if req.Text != nil {
				text = *req.Text
			}
			snap, ok := storedTree(w, c)
			if !ok {
				return
			}
			cursors := orchestra.Find(orchestra.CompileSelector(req.Selector), snap.Tree)
			if len(cursors) == 0 {
				writeCoded(w, http.StatusNotFound, codeNodeNotFound,
					"the selector matches no node in the stored tree, so it cannot be told whether the field is secure",
					"Test the selector against the tree, or ask for a fresh capture, then try again.")
				return
			}
			for _, cur := range cursors {
				secure = secure || orchestra.IsSecure(cur.Node)
			}
			if secure {
				text = "${" + securePlaceholderName + "}"
			}
		}
		step, err := buildStep(req.Action, sel, text)
		if err != nil {
			writeFailure(w, err)
			return
		}
		rendered, err := script.RenderStepYAML(step)
		if err != nil {
			writeFailure(w, invalidArgument(renderProblem(err), "Adjust the selector and try again."))
			return
		}
		out := stepYAMLResponse{YAML: rendered, Secure: secure}
		if secure {
			out.Placeholder = securePlaceholderName
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func isSupportedAction(action string) bool {
	for _, a := range supportedActions {
		if a == action {
			return true
		}
	}
	return false
}

func buildStep(action string, sel script.ScriptSelector, text string) (script.ScriptStep, error) {
	switch action {
	case actionTap:
		return &script.TapStep{Command: script.CommandTap, Selector: sel}, nil
	case actionAssertVisible:
		return &script.AssertVisibleStep{Command: script.CommandAssertVisible, Selector: sel}, nil
	case actionAssertNotVisible:
		return &script.AssertNotVisibleStep{Command: script.CommandAssertNotVisible, Selector: sel}, nil
	case actionAssertExists:
		return &script.AssertExistsStep{Command: script.CommandAssertExists, Selector: sel}, nil
	case actionWaitFor:
		return &script.WaitForStep{Command: script.CommandWaitFor, Selector: sel}, nil
	case actionType:
		return &script.TypeStep{Command: script.CommandType, Text: text, Into: &sel}, nil
	}
	return nil, invalidArgument("the action "+quote(action)+" is not supported", "Use one of: "+strings.Join(supportedActions, ", ")+".")
}

// renderProblem words a RenderStepYAML failure without the step.
func renderProblem(err error) string {
	switch {
	case errors.Is(err, script.ErrEmptySelector):
		return "the selector sets no field a script step can hold"
	case errors.Is(err, script.ErrUnsupportedStep):
		return "the step cannot be written as YAML"
	}
	return "the step is not valid"
}

func quote(s string) string { return `"` + s + `"` }
