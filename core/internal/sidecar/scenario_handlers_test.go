package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/wire"
)

// writeScenarioFixture stages a two-script scenario onto a temp
// directory: one script that passes, one that fails, with the
// scenario's default onFailure: "stop" so the run stops after the
// second script. It returns the scenario YAML (referencing the child
// scripts by name, exactly as RunScenario's loadScript resolves them)
// and the directory to pass as params.cwd.
func writeScenarioFixture(t *testing.T) (yaml string, cwd string) {
	t.Helper()
	dir := t.TempDir()
	pass := "appId: com.test\nname: pass\n---\n- launchApp\n- assertVisible: ok\n"
	fail := "appId: com.test\nname: fail\n---\n- launchApp\n- assertVisible: missing\n"
	if err := os.WriteFile(filepath.Join(dir, "pass.yml"), []byte(pass), 0o600); err != nil {
		t.Fatalf("writing pass.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail.yml"), []byte(fail), 0o600); err != nil {
		t.Fatalf("writing fail.yml: %v", err)
	}
	scenarioYaml := "name: fixture\nscripts:\n  - pass.yml\n  - fail.yml\n"
	return scenarioYaml, dir
}

func TestScript_RunScript_ScenarioMissingCwd_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterScriptFeature(ctx)

	yaml, _ := writeScenarioFixture(t)
	params, _ := json.Marshal(map[string]string{"yaml": yaml})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)

	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
	want := "scenario runs require params.cwd (directory of the scenario file)"
	if dispatchErr.Message != want {
		t.Fatalf("Dispatch() error message = %q, want %q", dispatchErr.Message, want)
	}
}

func TestScript_RunScript_Scenario_EmitsEventsAndWireShape(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	drv.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{},
		Children: []wire.TreeNodeWire{
			{Attributes: map[string]string{"text": "ok"}, Children: []wire.TreeNodeWire{}},
		},
	})
	RegisterScriptFeature(ctx)

	yaml, cwd := writeScenarioFixture(t)

	var mu sync.Mutex
	var eventNames []string
	var scenarioEvents []Event
	unsubscribe := ctx.Events.Subscribe(func(e Event) {
		mu.Lock()
		eventNames = append(eventNames, e.Event)
		switch e.Event {
		case "scenarioStarted", "scriptStarted", "scriptCompleted", "scenarioCompleted":
			scenarioEvents = append(scenarioEvents, e)
		}
		mu.Unlock()
	})
	defer unsubscribe()

	params, _ := json.Marshal(map[string]string{"yaml": yaml, "cwd": cwd})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "runScript", params)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	got, ok := result.(runScenarioResultWire)
	if !ok {
		t.Fatalf("Dispatch() result = %#v, want runScenarioResultWire", result)
	}
	if got.RunID == "" {
		t.Fatal("RunID is empty")
	}
	if got.Result.Ok {
		t.Fatalf("Result.Ok = true, want false (second child script fails)")
	}
	if got.Result.ScenarioName != "fixture" || got.Result.TotalScripts != 2 || got.Result.PassedScripts != 1 {
		t.Fatalf("Result = %+v, want scenarioName=fixture totalScripts=2 passedScripts=1", got.Result)
	}
	if len(got.Result.Scripts) != 2 {
		t.Fatalf("len(Result.Scripts) = %d, want 2", len(got.Result.Scripts))
	}
	first := got.Result.Scripts[0]
	if first.Status != "passed" || first.Result == nil || !first.Result.Ok || first.Result.ScriptName != "pass" {
		t.Fatalf("Scripts[0] = %+v, want passed with nested scriptResultWire scriptName=pass", first)
	}
	second := got.Result.Scripts[1]
	if second.Status != "failed" || second.Result == nil || second.Result.Ok {
		t.Fatalf("Scripts[1] = %+v, want failed with a nested scriptResultWire", second)
	}

	mu.Lock()
	defer mu.Unlock()
	// The scenario's two child scripts share one appId and run as a
	// single flow (no launched-app reset between them), so the second
	// script's own launchApp step is a dedup no-op the runner omits
	// entirely: it carries only the one stepStarted/stepCompleted pair
	// for its assertVisible step.
	wantNames := []string{
		"runStarted", "scenarioStarted",
		"scriptStarted", "stepStarted", "stepCompleted", "stepStarted", "stepCompleted", "scriptCompleted",
		"scriptStarted", "stepStarted", "stepCompleted", "scriptCompleted",
		"scenarioCompleted", "runCompleted",
	}
	if len(eventNames) != len(wantNames) {
		t.Fatalf("events = %v, want %v", eventNames, wantNames)
	}
	for i := range wantNames {
		if eventNames[i] != wantNames[i] {
			t.Fatalf("events[%d] = %q, want %q (full: %v)", i, eventNames[i], wantNames[i], eventNames)
		}
	}

	wantScenarioTypes := []string{"scenarioStarted", "scriptStarted", "scriptCompleted", "scriptStarted", "scriptCompleted", "scenarioCompleted"}
	if len(scenarioEvents) != len(wantScenarioTypes) {
		t.Fatalf("scenario events = %d, want %d", len(scenarioEvents), len(wantScenarioTypes))
	}
	for _, e := range scenarioEvents {
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatalf("marshaling %s payload: %v", e.Event, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshaling %s payload: %v", e.Event, err)
		}
		if decoded["runId"] != got.RunID {
			t.Fatalf("%s payload runId = %v, want %v", e.Event, decoded["runId"], got.RunID)
		}
		if decoded["type"] != e.Event {
			t.Fatalf("%s payload type = %v, want %v", e.Event, decoded["type"], e.Event)
		}
		switch e.Event {
		case "scenarioStarted":
			if _, has := decoded["scriptIndex"]; has {
				t.Fatalf("scenarioStarted payload has a stray scriptIndex key: %v", decoded)
			}
		case "scriptStarted":
			if _, has := decoded["scenarioName"]; has {
				t.Fatalf("scriptStarted payload has a stray scenarioName key: %v", decoded)
			}
		}
	}
}
