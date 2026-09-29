// Package report renders a run's result into a persisted report
// bundle: an HTML document, a versioned JSON document, and the
// screenshots either one references.
//
// ReportInput is an interface rather than a closed union because Go
// has no sum types: RenderJSON and RenderHTML type-switch over its
// three implementations, including ScriptReportInput, ScenarioReportInput,
// and SuiteReportInput, and return an error for any other
// implementation.
package report
