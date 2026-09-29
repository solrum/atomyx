package conformance

// SuiteRunOracleEntrySpec is one virtual suite entry file: a path and
// the YAML or scenario content at that path. suiteRunCorpus builds a
// SuiteRunCase's Entries from these, and runGoSuiteCase turns each one
// into a scenario.SuiteEntry for the Go suite runner.
type SuiteRunOracleEntrySpec struct {
	Path    string
	Content string
}
