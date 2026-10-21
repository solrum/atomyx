package runner

import (
	"context"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/script"
)

const (
	leakAlias  = "aliasV9z"
	leakNumber = "48271"
	leakEnum   = `"center"`
)

var failClosedEnv = map[string]string{
	"SECRET": envSecret, "ALIAS": leakAlias, "NUM": leakNumber, "DIR": "left", "KEY": "volumeUp", "ANCHOR": "center",
}

// failClosedCases cover every step kind the normalizer supports with a
// placeholder in each field that accepts one. A lost case has a
// placeholder in a slot that rejects the stand-in, so only its command
// name may be shown.
var failClosedCases = []struct {
	name    string
	steps   string
	lost    bool
	summary string
}{
	{name: "tap text", steps: `- tap: "${SECRET}"`, summary: `element.text = "${SECRET}"`},
	{name: "tap id", steps: `- tap: { id: "${SECRET}" }`, summary: `element.id = "${SECRET}"`},
	{name: "tap label", steps: `- tap: { label: "${SECRET}" }`, summary: `element.label = "${SECRET}"`},
	{name: "tap hint", steps: `- tap: { hint: "${SECRET}" }`, summary: `element.hint = "${SECRET}"`},
	{name: "tap role", steps: `- tap: { role: "${SECRET}" }`, summary: `element.role = "${SECRET}"`},
	{name: "type", steps: `- type: "${SECRET}"`},
	{name: "type into", steps: `- type: { into: { id: "${SECRET}" }, text: "${SECRET}" }`, summary: `element.id = "${SECRET}"`},
	{name: "waitFor", steps: `- waitFor: { text: "${SECRET}", timeout: 100 }`, summary: `element.text = "${SECRET}"`},
	{name: "assertVisible", steps: `- assertVisible: "${SECRET}"`, summary: `element.text = "${SECRET}"`},
	{name: "assertNotVisible", steps: `- assertNotVisible: { id: "${SECRET}" }`, summary: `element.id = "${SECRET}"`},
	{name: "assertExists", steps: `- assertExists: { hint: "${SECRET}" }`, summary: `element.hint = "${SECRET}"`},
	{name: "screenshot", steps: `- screenshot: "${SECRET}"`, summary: `"${SECRET}"`},
	{name: "swipe", steps: `- swipe: "${DIR}"`, summary: `"${DIR}"`},
	{name: "pressKey", steps: `- pressKey: "${KEY}"`, summary: `"${KEY}"`},
	{name: "sleep", steps: `- sleep: "${NUM}"`, lost: true},
	{name: "capture shorthand", steps: `- capture: "GET /${SECRET} as: ${ALIAS}"`, summary: `capture: ${ALIAS} ← "GET /${SECRET}"`},
	{name: "capture object", steps: `- capture: { pattern: "GET /${SECRET}", as: "${ALIAS}" }`, summary: `"GET /${SECRET}"`},
	{name: "assertApi", steps: `- assertApi: { from: "${ALIAS}", status: 200, body: { "$.token": "${SECRET}" } }`, summary: `$${ALIAS}`},
	{name: "extract", steps: `- extract: { from: "${ALIAS}", values: { token: "$.a.${SECRET}" } }`, summary: `$${ALIAS}`},
	{name: "read", steps: `- read: { selector: { id: "${SECRET}" }, field: text, as: "${ALIAS}" }`, summary: ``},
	{name: "assertExpr", steps: `- assertExpr: { expr: "'${SECRET}' == 'x'" }`},
	{name: "handle", steps: "- handle:\n    - when: { visible: \"${SECRET}\" }\n      do:\n        - tap: \"${SECRET}\"\n    - otherwise: skip", summary: `1 branch`},
	{name: "branch", steps: "- branch:\n    from: \"${ALIAS}\"\n    on:\n      - match: { body: { \"$.t\": \"${SECRET}\" } }\n        do:\n          - tap: \"${SECRET}\"\n    default:\n      - tap: \"${SECRET}\"", summary: `$${ALIAS}`},
	{name: "runFlow", steps: `- runFlow: { file: "${SECRET}.yaml", env: { K: "${SECRET}" } }`, summary: `"${SECRET}.yaml"`},
	{name: "pointer", steps: "- pointer:\n    touches:\n      - actions:\n          - down: { id: \"${SECRET}\" }\n          - up"},
	{name: "pointer anchor", steps: "- pointer:\n    touches:\n      - actions:\n          - down: { selector: { id: \"${SECRET}\" }, anchor: \"${ANCHOR}\" }\n          - up", lost: true},
	{name: "longPress", steps: "- longPress:\n    at: { id: \"${SECRET}\" }"},
	{name: "longPress anchor", steps: "- longPress:\n    at: { selector: { id: \"${SECRET}\" }, anchor: \"${ANCHOR}\" }", lost: true},
	{name: "pinch", steps: "- pinch:\n    center: { text: \"${SECRET}\" }\n    startGap: 40\n    endGap: 120"},
	{name: "rotate", steps: "- rotate:\n    center: { selector: { id: \"${SECRET}\" }, anchor: \"${ANCHOR}\" }\n    degrees: 90", lost: true},
	{name: "numeric placeholder in an object", steps: `- waitFor: { text: "${SECRET}", timeout: "${NUM}" }`, summary: `element.text = "${SECRET}"`},
}

func TestEveryStepKindKeepsExpandedValuesOutOfStepRecords(t *testing.T) {
	for _, tc := range failClosedCases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTestRunner()
			tr.driver.StageHierarchy(rootTree())
			evidence := newMemoryEvidence()
			tr.runner.deps.FailureCapture = NewFailureBundle(FailureBundleDeps{
				Orchestra: tr.orch, Clock: clock.NewManualClock(0), Logger: noopLogger{}, Sink: evidence,
			})
			def, err := script.ParseScript("appId: com.test\nproxy: required\n---\n"+tc.steps+"\n", failClosedEnv)
			if err != nil {
				t.Fatal(err)
			}
			var events []StepEvent
			tr.runner.deps.OnStep = func(e StepEvent) { events = append(events, e) }
			result, err := tr.runner.Run(context.Background(), def)
			if err != nil {
				t.Fatal(err)
			}

			var records []string
			for _, s := range result.Steps {
				records = append(records, detail(s))
			}
			var summaries []string
			for _, e := range events {
				records = append(records, e.Summary, detailOrEmpty(e.Detail))
				summaries = append(summaries, e.Summary)
				for _, tok := range e.Tokens {
					records = append(records, tok.Text)
				}
			}
			for _, name := range evidence.names() {
				if strings.HasSuffix(name, "log.jsonl") {
					records = append(records, string(evidence.file(name)))
				}
			}
			text := strings.Join(records, "\n")
			for _, leak := range []string{envSecret, leakAlias, leakNumber, leakEnum} {
				if strings.Contains(text, leak) {
					t.Fatalf("a step record carries %q:\n%s", leak, text)
				}
			}

			command := strings.SplitN(strings.TrimPrefix(tc.steps, "- "), ":", 2)[0]
			if tc.lost {
				for _, s := range summaries {
					if s != command {
						t.Fatalf("summary = %q, want the command name %q alone", s, command)
					}
				}
				for _, s := range result.Steps {
					if d := detail(s); d != command+" failed" && d != command+" completed" {
						t.Fatalf("detail = %q, want the command name and the outcome", d)
					}
				}
			} else if !strings.Contains(text, tc.summary) {
				t.Fatalf("step records lack %q:\n%s", tc.summary, text)
			}
		})
	}
}

const runtimeSecret = "rtS3cr3tV4lue88"

// TestRunTimeResolvedSecretKeepsOutOfEveryRecord substitutes a secret read
// during the run into a type step, a selector and an httpRequest header,
// and checks that the substitution happened while no record of the run
// carries the value.
func TestRunTimeResolvedSecretKeepsOutOfEveryRecord(t *testing.T) {
	var sent []HTTPRequest
	tr := newHTTPTestRunner(doerFunc(func(_ context.Context, req HTTPRequest) (HTTPResponse, error) {
		sent = append(sent, req)
		return HTTPResponse{Status: 200, Body: []byte(`{}`)}, nil
	}))
	var gated []script.ScriptStep
	deps := tr.runner.deps
	deps.StepGate = &funcGate{fn: func(s GateStep, _ PausedRun) error {
		gated = append(gated, s.Step)
		return nil
	}}
	evidence := newMemoryEvidence()
	deps.FailureCapture = NewFailureBundle(FailureBundleDeps{
		Orchestra: tr.orch, Clock: clock.NewManualClock(0), Logger: noopLogger{}, Sink: evidence,
	})
	var events []StepEvent
	deps.OnStep = func(e StepEvent) { events = append(events, e) }
	tr.runner = NewScriptRunner(deps)
	tr.driver.StageHierarchy(typeTree(secureField("pw", runtimeSecret)))

	def, err := script.ParseScript("appId: com.test\nproxy: required\n---\n"+
		"- read: { selector: { id: pw }, field: value, as: pwd }\n"+
		"- type: \"${pwd}\"\n"+
		"- httpRequest: { url: \"https://api.example.com/x\", headers: { X-Token: \"${pwd}\" } }\n"+
		"- tap: { text: \"${pwd}\" }\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatal(err)
	}

	if typed := typedTexts(tr); len(typed) != 1 || typed[0] != runtimeSecret {
		t.Fatalf("typed %q, want the substituted secret", typed)
	}
	if len(sent) != 1 || sent[0].Headers["X-Token"] != runtimeSecret {
		t.Fatalf("requests = %+v, want the secret in the header", sent)
	}
	if result.Ok || result.FailedAtStep == nil || *result.FailedAtStep != 3 {
		t.Fatalf("steps = %s, want the tap of an absent element to fail", debugSteps(result.Steps))
	}

	var records []string
	for _, s := range result.Steps {
		records = append(records, detail(s))
	}
	for _, e := range events {
		records = append(records, e.Summary, detailOrEmpty(e.Detail))
		for _, tok := range e.Tokens {
			records = append(records, tok.Text)
		}
	}
	for _, step := range gated {
		records = append(records, SummarizeStep(step).Text)
		shown, err := script.RenderStepYAML(script.AuthoredOf(step))
		if err == nil {
			records = append(records, shown)
		}
	}
	for _, name := range evidence.names() {
		records = append(records, string(evidence.file(name)))
	}
	text := strings.Join(records, "\n")
	if strings.Contains(text, runtimeSecret) {
		t.Fatalf("a record carries the substituted secret:\n%s", text)
	}
	if !strings.Contains(text, `element.text = "${pwd}"`) {
		t.Fatalf("records lack the selector as written:\n%s", text)
	}
}
