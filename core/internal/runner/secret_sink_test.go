package runner

import (
	"context"
	"sort"
	"sync"
	"testing"

	"dev.atomyx/internal/orchestra"
)

type collectingSink struct {
	mu     sync.Mutex
	values []string
}

func (c *collectingSink) Add(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = append(c.values, value)
}

func (c *collectingSink) sorted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]string(nil), c.values...)
	sort.Strings(out)
	return out
}

func runnerWithSink(sink SecretSink) *testRunner {
	tr := newTestRunner()
	deps := tr.runner.deps
	deps.SecretSink = sink
	tr.runner = NewScriptRunner(deps)
	return tr
}

func requireSunk(t *testing.T, sink *collectingSink, want ...string) {
	t.Helper()
	got := sink.sorted()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("sink received %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sink received %v, want %v", got, want)
		}
	}
}

func TestSecretSink_ReceivesExternalValuesAndNotScriptLiterals(t *testing.T) {
	sink := &collectingSink{}
	tr := runnerWithSink(sink)

	runWithEnv(t, tr, "appId: com.test\nenv:\n  LITERAL: written-in-script\n---\n- screenshot: a\n", map[string]string{"TOKEN": "external-value"})

	requireSunk(t, sink, "external-value")
}

func TestSecretSink_ReceivesASecureReadAndNotAPlainRead(t *testing.T) {
	sink := &collectingSink{}
	tr := runnerWithSink(sink)
	tr.driver.StageHierarchy(rootTree(secureField("pw", "topsecret"), button("go", "Go", "0,0,10,10")))

	run(t, tr, "appId: com.test\n---\n"+
		"- read: { selector: { id: pw }, field: value, as: pwd }\n"+
		"- read: { selector: { id: go }, field: text, as: label }\n")

	requireSunk(t, sink, "topsecret")
}

func TestSecretSink_ReceivesAValueTaggedInAPausedScope(t *testing.T) {
	sink := &collectingSink{}
	gate := &funcGate{fn: func(s GateStep, paused PausedRun) error {
		if s.StepIndex != 1 {
			return nil
		}
		step := mustParse(t, "appId: com.test\n---\n- read: { selector: { id: pw }, field: value, as: fromAct }\n").Steps[0]
		if _, err := paused.ExecuteStep(context.Background(), step); err != nil {
			t.Fatalf("ExecuteStep: %v", err)
		}
		return nil
	}}
	tr := gatedRunner(gate, "")
	deps := tr.runner.deps
	deps.SecretSink = sink
	tr.runner = NewScriptRunner(deps)
	tr.driver.StageHierarchy(rootTree(secureField("pw", "topsecret")))

	run(t, tr, "appId: com.test\n---\n- screenshot: a\n- screenshot: b\n")

	requireSunk(t, sink, "topsecret")
}

func TestRunSecrets_MasksOnlyValuesAddedSoFar(t *testing.T) {
	secrets := NewRunSecrets()
	text := "token hunter2hunter2 and later-value"
	if got := secrets.String(text); got != text {
		t.Fatalf("an empty set changed the text: %q", got)
	}
	secrets.Add("hunter2hunter2")
	if got := secrets.String(text); got != "token "+orchestra.SecureMask+" and later-value" {
		t.Fatalf("got %q", got)
	}
	secrets.Add("later-value")
	secrets.Add("")
	if got := secrets.String(text); got != "token "+orchestra.SecureMask+" and "+orchestra.SecureMask {
		t.Fatalf("a value added later was not masked: %q", got)
	}
}

func TestRunSecrets_MasksAShortValueOnlyAsAWholeWord(t *testing.T) {
	secrets := NewRunSecrets()
	secrets.Add("ab1")

	if got := secrets.String("code ab1 and ab123"); got != "code "+orchestra.SecureMask+" and ab123" {
		t.Fatalf("got %q", got)
	}
}

func TestRunSecrets_AddExternalSkipsCommittedNames(t *testing.T) {
	secrets := NewRunSecrets()
	secrets.AddExternal(map[string]string{"TOKEN": "external-value", "appId": "com.example.app"}, map[string]bool{"appId": true})

	text := "external-value com.example.app"
	if got := secrets.String(text); got != orchestra.SecureMask+" com.example.app" {
		t.Fatalf("got %q", got)
	}
}

func TestRunSecrets_IsSafeForConcurrentUse(t *testing.T) {
	secrets := NewRunSecrets()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				secrets.Add("value-" + string(rune('a'+j%20)) + "-secret")
				_ = secrets.String("value-a-secret")
			}
		}()
	}
	wg.Wait()
	if got := secrets.String("value-a-secret"); got != orchestra.SecureMask {
		t.Fatalf("got %q", got)
	}
}
