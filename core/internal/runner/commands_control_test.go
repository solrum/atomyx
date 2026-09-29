package runner

import (
	"context"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/script"
)

func TestHandleCommand_MatchesFirstVisibleBranch(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("otp", "Verify it's you", "0,0,10,10")))

	yaml := "appId: com.test\n---\n" +
		"- handle:\n" +
		"    - when:\n" +
		"        visible: \"Verify it's you\"\n" +
		"      do:\n" +
		"        - screenshot: otp-branch\n" +
		"    - when:\n" +
		"        visible: Dashboard\n" +
		"      do:\n" +
		"        - screenshot: dashboard-branch\n"
	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "otp-branch" {
		t.Fatalf("got screenshots %+v, want [otp-branch]", shots)
	}
}

// handle's `timeout` field is not reachable through the YAML array
// shorthand in either engine (the TypeScript step-normalizer's own
// `handle` case never reads a "timeout" entry either), so these two
// tests construct a HandleStep directly instead of going through
// ParseScript.

func TestHandleCommand_OtherwiseSkip(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	timeout := 150
	otherwise := "skip"

	def := &script.ScriptDefinition{
		AppID: "com.test",
		Env:   map[string]string{},
		Steps: []script.ScriptStep{&script.HandleStep{
			Command: script.CommandHandle,
			Branches: []script.HandleBranch{{
				When: script.HandleCondition{Visible: strAsStringOrSelector("Nope")},
				Do:   script.StepsOrFile{Steps: []script.ScriptStep{&script.BackStep{Command: script.CommandBack}}},
			}},
			Otherwise: &otherwise,
			Timeout:   &timeout,
		}},
	}
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != "handle: no branch matched, skipped" {
		t.Fatalf("got detail %q", got)
	}
}

func TestHandleCommand_OtherwiseFail(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	timeout := 150

	def := &script.ScriptDefinition{
		AppID: "com.test",
		Env:   map[string]string{},
		Steps: []script.ScriptStep{&script.HandleStep{
			Command: script.CommandHandle,
			Branches: []script.HandleBranch{{
				When: script.HandleCondition{Visible: strAsStringOrSelector("Nope")},
				Do:   script.StepsOrFile{Steps: []script.ScriptStep{&script.BackStep{Command: script.CommandBack}}},
			}},
			Timeout: &timeout,
		}},
	}
	result, err := tr.runner.Run(context.Background(), def)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := "handle: no branch matched within 150ms (otherwise: fail)"
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func strAsStringOrSelector(s string) *script.StringOrSelector {
	return &script.StringOrSelector{Str: &s}
}

func TestHandleCommand_DoResolvedFromFile(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree(button("otp", "Verify", "0,0,10,10")))
	absPath, _ := filepath.Abs("flows/otp.yml")
	tr.loader.stage(absPath, "- screenshot: from-file\n")

	yaml := "appId: com.test\n---\n" +
		"- handle:\n" +
		"    - when:\n" +
		"        visible: Verify\n" +
		"      do: flows/otp.yml\n"
	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	shots := result.Artifacts.Screenshots()
	if len(shots) != 1 || shots[0].Label != "from-file" {
		t.Fatalf("got screenshots %+v, want [from-file]", shots)
	}
}

func TestRunFlowCommand_HappyPath(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	absPath, _ := filepath.Abs("flows/cleanup.yml")
	tr.loader.stage(absPath, "name: cleanup\n---\n- screenshot: cleaned\n- back\n")

	yaml := "appId: com.test\n---\n- runFlow:\n    file: flows/cleanup.yml\n"
	result := run(t, tr, yaml)
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if got := detail(lastStep(t, result)); got != `Sub-flow "cleanup" completed (2 steps)` {
		t.Fatalf("got detail %q", got)
	}
}

func TestRunFlowCommand_FileNotFound(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())

	yaml := "appId: com.test\n---\n- runFlow:\n    file: flows/missing.yml\n"
	result := run(t, tr, yaml)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	absPath, _ := filepath.Abs("flows/missing.yml")
	want := "Sub-flow file not found: " + absPath
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}

func TestRunFlowCommand_PropagatesSubFlowFailure(t *testing.T) {
	tr := newTestRunner()
	tr.driver.StageHierarchy(rootTree())
	absPath, _ := filepath.Abs("flows/broken.yml")
	tr.loader.stage(absPath, "name: broken\n---\n- assertVisible: Nope\n")

	yaml := "appId: com.test\n---\n- runFlow:\n    file: flows/broken.yml\n"
	result := run(t, tr, yaml)
	if result.Ok {
		t.Fatalf("got Ok=true, want false")
	}
	want := `Sub-flow "broken" failed: Element not visible on screen`
	if got := detail(lastStep(t, result)); got != want {
		t.Fatalf("got detail %q, want %q", got, want)
	}
}
