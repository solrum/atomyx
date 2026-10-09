package scenario

import (
	"context"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// persistingArtifacts models a host that writes each screenshot the
// moment it is added: persisted holds what is already on disk.
type persistingArtifacts struct {
	persisted   *[]string
	screenshots []runner.Screenshot
}

func (p *persistingArtifacts) AddScreenshot(label string, data []byte, mimeType string) {
	*p.persisted = append(*p.persisted, label)
	p.screenshots = append(p.screenshots, runner.Screenshot{Label: label, Data: data, MimeType: mimeType})
}

func (p *persistingArtifacts) Screenshots() []runner.Screenshot { return p.screenshots }

func (*persistingArtifacts) WriteEvidence(string, []byte) error { return nil }

type childFactoryRecorder struct {
	targets   []ChildTarget
	persisted []string
}

func (c *childFactoryRecorder) factory(target ChildTarget) ChildArtifacts {
	c.targets = append(c.targets, target)
	return &persistingArtifacts{persisted: &c.persisted}
}

const shotYaml = "appId: com.test\nname: shot\n---\n- launchApp\n- screenshot: first\n"

func TestScenarioRunner_PersistsEachChildScreenshotBeforeTheScenarioEnds(t *testing.T) {
	recorder := &childFactoryRecorder{}
	var persistedAtFirstChildEnd []string
	scenarioEnded := false
	def, err := script.ParseScenario("name: shots\nscripts:\n  - a.yml\n  - b.yml\n")
	if err != nil {
		t.Fatal(err)
	}

	r := NewScenarioRunner(ScenarioRunnerDeps{
		Orchestra:      newFixture(t),
		Clock:          orchestra.NewFakeClock(0),
		Logger:         noopLogger{},
		Commands:       runner.DefaultCommands(),
		LoadScript:     MemoryScriptLoader(map[string]string{"a.yml": shotYaml, "b.yml": shotYaml}),
		ChildArtifacts: recorder.factory,
		OnScenarioEvent: func(e ScenarioEvent) {
			switch {
			case e.Type == ScriptCompleted && e.ScriptIndex == 0:
				persistedAtFirstChildEnd = append([]string(nil), recorder.persisted...)
			case e.Type == ScenarioCompleted:
				scenarioEnded = true
			}
		},
	})
	if _, err := r.Run(context.Background(), def); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !scenarioEnded {
		t.Fatal("scenario never ended")
	}
	if len(persistedAtFirstChildEnd) != 1 || persistedAtFirstChildEnd[0] != "first" {
		t.Fatalf("persisted when the first child ended = %v, want the first child's screenshot", persistedAtFirstChildEnd)
	}
	want := []ChildTarget{
		{EntryIndex: NoPosition, ScriptIndex: 0, ScriptPath: "a.yml"},
		{EntryIndex: NoPosition, ScriptIndex: 1, ScriptPath: "b.yml"},
	}
	if len(recorder.targets) != 2 || recorder.targets[0] != want[0] || recorder.targets[1] != want[1] {
		t.Fatalf("targets = %+v, want %+v", recorder.targets, want)
	}
}

func TestSuiteRunner_ChildTargetsCarryTheEntryPosition(t *testing.T) {
	recorder := &childFactoryRecorder{}
	entries := []SuiteEntry{
		{Path: "suite/a.yml", Content: shotYaml},
		{Path: "suite/s.yml", Content: "name: s\nscripts:\n  - c.yml\n"},
	}

	r := NewSuiteRunner(SuiteRunnerDeps{
		Orchestra:      newFixture(t),
		Clock:          orchestra.NewFakeClock(0),
		Logger:         noopLogger{},
		Commands:       runner.DefaultCommands(),
		ChildArtifacts: recorder.factory,
		CreateLoadScript: func(string) LoadScript {
			return MemoryScriptLoader(map[string]string{"c.yml": shotYaml})
		},
	})
	if _, err := r.Run(context.Background(), entries, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []ChildTarget{
		{EntryIndex: 0, EntryPath: "suite/a.yml", Attempt: 1, ScriptIndex: NoPosition},
		{EntryIndex: 1, EntryPath: "suite/s.yml", Attempt: 1, ScriptIndex: 0, ScriptPath: "c.yml"},
	}
	if len(recorder.targets) != 2 || recorder.targets[0] != want[0] || recorder.targets[1] != want[1] {
		t.Fatalf("targets = %+v, want %+v", recorder.targets, want)
	}
	if len(recorder.persisted) != 2 {
		t.Fatalf("persisted = %v, want one screenshot per script", recorder.persisted)
	}
}
