package conformance

// SuiteRunCase is one differential suite-runner case: an ordered list
// of resolved entries (script or scenario, dispatched by content, not
// tagged), the child scripts a scenario entry's own `scripts` list
// resolves against (keyed by "<dirname(entry.Path)>/<childPath>"), and
// an optional suite name. Every case shares scenarioRunHierarchy as
// its driver program.
type SuiteRunCase struct {
	Name        string
	Description string
	SuiteName   *string
	Entries     []SuiteRunOracleEntrySpec
	Scripts     map[string]string
}

func strPtrCorpus(s string) *string { return &s }

// suiteRunCorpus returns every suite-runner differential case.
// Coverage: a suite mixing a script entry and a scenario entry, all
// passing; and a suite where one entry fails without stopping the
// remaining entries from running.
func suiteRunCorpus() []SuiteRunCase {
	return []SuiteRunCase{
		{
			Name:        "mixed-script-and-scenario-all-pass",
			Description: "a suite mixing a plain script entry and a scenario entry, both passing, reports ok true with every entry passed",
			SuiteName:   strPtrCorpus("mixed suite"),
			Entries: []SuiteRunOracleEntrySpec{
				{Path: "scripts/a.yml", Content: scenarioRunPassYaml},
				{Path: "scenarios/s1.yml", Content: "name: s1\nscripts:\n  - child-pass.yml\n"},
			},
			Scripts: map[string]string{
				"scenarios/child-pass.yml": scenarioRunPassYaml,
			},
		},
		{
			Name:        "one-failing-entry-does-not-stop-the-rest",
			Description: "a failing entry in the middle of a suite does not stop later entries from running, unlike a scenario's own onFailure: stop",
			Entries: []SuiteRunOracleEntrySpec{
				{Path: "scripts/a.yml", Content: scenarioRunPassYaml},
				{Path: "scripts/b.yml", Content: scenarioRunFailYaml},
				{Path: "scenarios/s1.yml", Content: "name: s1\nscripts:\n  - child-pass.yml\n"},
			},
			Scripts: map[string]string{
				"scenarios/child-pass.yml": scenarioRunPassYaml,
			},
		},
	}
}
