package scenario

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// locationGate records the location of every top-level step it sees.
type locationGate struct {
	mu        sync.Mutex
	locations []string
}

func (g *locationGate) BeforeStep(_ context.Context, step runner.GateStep, _ runner.PausedRun) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if step.Location != "" {
		g.locations = append(g.locations, step.Location)
	}
	return nil
}

func TestScenarioChildrenReportTheirOwnAbsoluteSourceToTheGate(t *testing.T) {
	dir := t.TempDir()
	def, err := script.ParseScenario("name: s\nscripts:\n  - a.yml\n  - sub/b.yml\n")
	if err != nil {
		t.Fatal(err)
	}
	gate := &locationGate{}
	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra:  newFixture(t),
		Clock:      orchestra.NewFakeClock(0),
		Logger:     noopLogger{},
		Commands:   runner.DefaultCommands(),
		StepGate:   gate,
		SourceDir:  dir,
		LoadScript: MemoryScriptLoader(map[string]string{"a.yml": passYaml, "sub/b.yml": passYaml}),
	})
	if _, err := r.Run(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	if len(gate.locations) == 0 {
		t.Fatal("the gate saw no step")
	}
	first, last := gate.locations[0], gate.locations[len(gate.locations)-1]
	if !strings.HasPrefix(first, filepath.Join(dir, "a.yml")+":") || !strings.HasPrefix(last, filepath.Join(dir, "sub", "b.yml")+":") {
		t.Fatalf("steps reported %q then %q, want a.yml first and sub/b.yml last, both under %s", first, last, dir)
	}
}

func TestBreakScriptsParsesEveryChildAtItsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	def, _ := script.ParseScenario("name: s\nenv:\n  K: v\nscripts:\n  - a.yml\n")
	got, err := BreakScripts(def, nil, MemoryScriptLoader(map[string]string{"a.yml": passYaml}), dir)
	if err != nil || len(got) != 1 || got[0].Path != filepath.Join(dir, "a.yml") || got[0].Definition == nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if _, err := BreakScripts(def, nil, MemoryScriptLoader(map[string]string{}), dir); err == nil {
		t.Fatal("a child that cannot be loaded was accepted")
	}
}
