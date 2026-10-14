package scenario

import (
	"context"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

func TestCommittedChildNames(t *testing.T) {
	got := committedChildNames(
		map[string]string{"KEPT": "a", "OVERRIDDEN": "b"},
		map[string]string{"OVERRIDDEN": "c", "FLAG": "d", "CONFIG": "e"},
		map[string]bool{"CONFIG": true},
	)
	want := map[string]bool{"KEPT": true, "CONFIG": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("got %v, missing %q", got, name)
		}
	}
}

type collectingSecretSink struct{ values []string }

func (c *collectingSecretSink) Add(value string) { c.values = append(c.values, value) }

func TestScenarioAndSuiteRunnersFeedTheSinkOfEveryChildScript(t *testing.T) {
	const yaml = "appId: com.test\n---\n- screenshot: a\n"
	env := map[string]string{"TOKEN": "external-value"}

	t.Run("scenario", func(t *testing.T) {
		sink := &collectingSecretSink{}
		def, err := script.ParseScenario("name: s\nscripts:\n  - a.yml\n")
		if err != nil {
			t.Fatal(err)
		}
		r := NewScenarioRunner(ScenarioRunnerDeps{
			Orchestra: newFixture(t), Clock: orchestra.NewFakeClock(0), Logger: noopLogger{},
			Commands: runner.DefaultCommands(), ExternalEnv: env, SecretSink: sink,
			LoadScript: MemoryScriptLoader(map[string]string{"a.yml": yaml}),
		})
		if _, err := r.Run(context.Background(), def); err != nil {
			t.Fatal(err)
		}
		if len(sink.values) != 1 || sink.values[0] != "external-value" {
			t.Fatalf("sink received %v", sink.values)
		}
	})

	t.Run("suite script and scenario entries", func(t *testing.T) {
		sink := &collectingSecretSink{}
		r := NewSuiteRunner(SuiteRunnerDeps{
			Orchestra: newFixture(t), Clock: orchestra.NewFakeClock(0), Logger: noopLogger{},
			Commands: runner.DefaultCommands(), ExternalEnv: env, SecretSink: sink,
			CreateLoadScript: func(string) LoadScript {
				return MemoryScriptLoader(map[string]string{"c.yml": yaml})
			},
		})
		_, err := r.Run(context.Background(), []SuiteEntry{
			{Path: "suite/a.yml", Content: yaml},
			{Path: "suite/s.yml", Content: "name: s\nscripts:\n  - c.yml\n"},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sink.values) != 2 {
			t.Fatalf("sink received %v, want one value per script", sink.values)
		}
	})
}
