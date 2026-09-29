package conformance

import "dev.atomyx/internal/wire"

// scenarioRunHierarchy is the single static accessibility tree every
// scenario-run corpus case stages: one visible element whose text is
// "ok". assertVisible with no timeout is an instant check (see
// internal/runner/assert_visible_command.go), and mock.Driver returns
// the last-staged tree repeatedly once its queue is exhausted, so one
// staged tree is enough to drive any number of child scripts without
// needing a per-step queue.
var scenarioRunHierarchy = wire.TreeNodeWire{
	Attributes: map[string]string{},
	Children: []wire.TreeNodeWire{
		{Attributes: map[string]string{"text": "ok"}, Children: []wire.TreeNodeWire{}},
	},
}

// scenarioRunPassYaml is a child script that always passes: launchApp
// then assertVisible against the corpus's staged "ok" element.
const scenarioRunPassYaml = "appId: com.test\nname: pass\n---\n- launchApp\n- assertVisible: ok\n"

// scenarioRunFailYaml is a child script that always fails: it asserts
// visible against text the staged hierarchy never carries.
const scenarioRunFailYaml = "appId: com.test\nname: fail\n---\n- launchApp\n- assertVisible: missing\n"

// ScenarioRunCase is one differential scenario-runner case: a
// scenario YAML document, its child scripts keyed by the exact path
// the scenario's `scripts` list names, and the external environment
// (if any) a caller would supply on top of the scenario's own `env`.
// Every case shares scenarioRunHierarchy as its driver program.
type ScenarioRunCase struct {
	Name         string
	Description  string
	ScenarioYaml string
	Scripts      map[string]string
	ExternalEnv  map[string]string
}

// scenarioRunCorpus returns every scenario-runner differential case.
// Coverage: every script passing; a middle script failing with the
// default onFailure ("stop"), leaving the remaining script skipped;
// onFailure "continue", running every script regardless of the
// earlier failure; and externalEnv overriding a value the scenario's
// own env pins for its child scripts.
func scenarioRunCorpus() []ScenarioRunCase {
	return []ScenarioRunCase{
		{
			Name:        "all-pass",
			Description: "every child script passes, so the scenario passes with passedScripts equal to totalScripts",
			ScenarioYaml: "name: all pass\n" +
				"scripts:\n  - pass1.yml\n  - pass2.yml\n",
			Scripts: map[string]string{
				"pass1.yml": scenarioRunPassYaml,
				"pass2.yml": scenarioRunPassYaml,
			},
		},
		{
			Name:        "stop-on-failure-skips-remainder",
			Description: "a middle script failing under the default onFailure (stop) skips every remaining script without loading or running it",
			ScenarioYaml: "name: stop on failure\n" +
				"scripts:\n  - pass.yml\n  - fail.yml\n  - pass2.yml\n",
			Scripts: map[string]string{
				"pass.yml":  scenarioRunPassYaml,
				"fail.yml":  scenarioRunFailYaml,
				"pass2.yml": scenarioRunPassYaml,
			},
		},
		{
			Name:        "continue-on-failure-runs-every-script",
			Description: "onFailure: continue runs every script regardless of an earlier failure, reporting every outcome in one pass",
			ScenarioYaml: "name: continue on failure\n" +
				"onFailure: continue\n" +
				"scripts:\n  - pass.yml\n  - fail.yml\n  - pass2.yml\n",
			Scripts: map[string]string{
				"pass.yml":  scenarioRunPassYaml,
				"fail.yml":  scenarioRunFailYaml,
				"pass2.yml": scenarioRunPassYaml,
			},
		},
		{
			Name:        "external-env-overrides-scenario-env",
			Description: "externalEnv wins over the scenario's own env for a child script's ${var} interpolation",
			ScenarioYaml: "name: env override\n" +
				"env:\n  x: \"1\"\n" +
				"scripts:\n  - env-check.yml\n",
			Scripts: map[string]string{
				"env-check.yml": "appId: com.test\nname: env check\n---\n- launchApp\n- assertVisible: \"${x}\"\n",
			},
			ExternalEnv: map[string]string{"x": "ok"},
		},
	}
}
