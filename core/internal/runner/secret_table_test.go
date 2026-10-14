package runner

import (
	"context"
	"testing"

	"dev.atomyx/internal/wire"
)

// snapshotAtLastStep runs yaml with a gate that records the variables
// seen before the final step.
func snapshotAtLastStep(t *testing.T, tr *testRunner, yaml string, lastIndex int) VariableSnapshot {
	t.Helper()
	return snapshotAtLastStepWithEnv(t, tr, yaml, nil, lastIndex)
}

func snapshotAtLastStepWithEnv(t *testing.T, tr *testRunner, yaml string, env map[string]string, lastIndex int) VariableSnapshot {
	t.Helper()
	var snap VariableSnapshot
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex == lastIndex {
			snap = run.Variables()
		}
		return nil
	}}
	deps := tr.runner.deps
	deps.StepGate = gate
	tr.runner = NewScriptRunner(deps)
	result := runWithEnv(t, tr, yaml, env)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	return snap
}

func secureField(id, value string) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: map[string]string{
		"id": id, "value": value, "bounds": "0,0,10,10", wire.AttrSecure: wire.AttrSecureTrue,
	}}
}

func TestSecretTable_ExternalEntriesAreSecretAndScriptLiteralsAreNot(t *testing.T) {
	tr := newTestRunner()
	snap := snapshotAtLastStepWithEnv(t, tr,
		"appId: com.test\nenv:\n  LITERAL: written-in-script\n---\n- screenshot: a\n- screenshot: b\n",
		map[string]string{"PASSWORD": "hunter2hunter2"}, 1)
	if !snap.Secret["PASSWORD"] {
		t.Fatalf("external entry not tagged: %+v", snap.Secret)
	}
	if snap.Secret["LITERAL"] {
		t.Fatalf("script literal tagged: %+v", snap.Secret)
	}
}

func TestSecretTable_SecureReadIsSecretAndPlainReadIsNot(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(secureField("pw", "topsecret"), button("go", "Go", "0,0,10,10")))
	yaml := "appId: com.test\n---\n" +
		"- read: { selector: { id: pw }, field: value, as: pwd }\n" +
		"- read: { selector: { id: go }, field: text, as: label }\n" +
		"- screenshot: last\n"
	snap := snapshotAtLastStep(t, tr, yaml, 2)
	if !snap.Secret["pwd"] {
		t.Fatalf("secure read not tagged: %+v", snap.Secret)
	}
	if snap.Secret["label"] {
		t.Fatalf("plain read tagged: %+v", snap.Secret)
	}
}

func TestSecretTable_PlainReadOverSecretClearsTag(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("go", "Go", "0,0,10,10")))
	yaml := "appId: com.test\n---\n" +
		"- read: { selector: { id: go }, field: text, as: v }\n" +
		"- screenshot: last\n"
	snap := snapshotAtLastStepWithEnv(t, tr, yaml, map[string]string{"v": "hunter2hunter2"}, 1)
	if snap.Secret["v"] {
		t.Fatalf("overwritten variable keeps its tag: %+v", snap.Secret)
	}
}

func TestSecretTable_ExtractTagsTokenShapedValuesOnly(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	tr.capture.queue = []CapturedRequest{{Method: "POST", URL: "/login", Status: 200, Body: map[string]any{
		"token": "0123456789abcdef0123456789abcdef", "name": "alice",
	}}}
	yaml := "appId: com.test\nproxy: required\n---\n" +
		"- capture: \"POST /login as: r\"\n" +
		"- extract: { from: r, values: { tok: $.body.token, who: $.body.name } }\n" +
		"- screenshot: last\n"
	snap := snapshotAtLastStep(t, tr, yaml, 2)
	if !snap.Secret["tok"] || snap.Secret["who"] {
		t.Fatalf("got tags %+v, want only tok", snap.Secret)
	}
}

func TestSecretTable_ChildScopeCopiesTableAndKeepsRunClean(t *testing.T) {
	var before, after VariableSnapshot
	var childTagged bool
	gate := &funcGate{fn: func(s GateStep, run PausedRun) error {
		if s.StepIndex != 1 {
			return nil
		}
		before = run.Variables()
		step := mustParse(t, "appId: com.test\n---\n- read: { selector: { id: pw }, field: value, as: fromAct }\n").Steps[0]
		if _, err := run.ExecuteStep(context.Background(), step); err != nil {
			t.Fatalf("ExecuteStep: %v", err)
		}
		after = run.Variables()
		return nil
	}}
	tr := gatedRunner(gate, "")
	tr.driver.StageHierarchy(rootTree(secureField("pw", "topsecret")))
	runWithEnv(t, tr, "appId: com.test\n---\n- screenshot: a\n- screenshot: b\n", map[string]string{"seed": "one"})
	childTagged = before.Secret["seed"]
	if !childTagged {
		t.Fatalf("snapshot lost the seeded tag: %+v", before.Secret)
	}
	if after.Secret["fromAct"] || len(after.Secret) != len(before.Secret) {
		t.Fatalf("child scope leaked a tag into the run: %+v", after.Secret)
	}
}

func TestSecretTable_SnapshotCopiesTags(t *testing.T) {
	src := map[string]bool{"a": true}
	snap := snapshotVariables(nil, src)
	snap.Secret["b"] = true
	if len(src) != 1 {
		t.Fatalf("snapshot aliases the table: %+v", src)
	}
}

func TestSecretTable_StateReadOverSecretClearsTag(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(focusedButton("go", "Go", "0,0,10,10")))
	yaml := "appId: com.test\n---\n" +
		"- read: { selector: { id: go }, field: focused, as: v }\n" +
		"- screenshot: last\n"
	snap := snapshotAtLastStepWithEnv(t, tr, yaml, map[string]string{"v": "hunter2hunter2"}, 1)
	if snap.Secret["v"] {
		t.Fatalf("overwritten variable keeps its tag: %+v", snap.Secret)
	}
}
