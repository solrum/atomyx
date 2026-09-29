package report

// typeStepCommand is the runner-reported StepResult.Command value for
// a script's type step, mirrored here as a plain string because
// report cannot depend on the script package that defines the
// canonical constant: report renders the runner's already-flattened
// step results, not script step types.
const typeStepCommand = "type"

// redactedTypeDetail replaces the persisted Detail a successful type
// step reports, because that Detail otherwise carries the literal
// value the step typed, verbatim, into report.json and report.html. A
// typed value can be a secret resolved from a config environment
// variable, and this package has no way to tell a secret from an
// ordinary typed value once it reaches a step result, so every
// successful type step's Detail is replaced here rather than
// persisted unchanged.
const redactedTypeDetail = "Typed into the field (value redacted from the report)"

// redactStepDetail returns the Detail a rendered report persists for
// one step. It leaves detail unchanged except for a successful type
// step, where it substitutes redactedTypeDetail: a failed type step's
// Detail names the failure reason, never the typed value, so it
// carries nothing to redact.
func redactStepDetail(command string, ok bool, detail *string) *string {
	if command != typeStepCommand || !ok || detail == nil {
		return detail
	}
	redacted := redactedTypeDetail
	return &redacted
}
