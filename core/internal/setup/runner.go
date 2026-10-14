package setup

import (
	"context"
	"fmt"
)

// Run executes the registered steps in order and reports each outcome.
//
// In ModeCheck only Step.Check is called. In ModeFix a step that
// reports StatusManual and implements Fixer gets one Fix call, and its
// result replaces the check result. A step whose prerequisite did not
// end satisfied is not run and is reported StatusSkipped with the
// prerequisite named. A cancelled ctx skips every step not yet run.
func Run(ctx context.Context, reg *Registry, mode Mode) Report {
	entries := reg.Entries()
	report := Report{Steps: make([]StepReport, 0, len(entries))}
	blocked := map[string]bool{}
	outcome := map[string]Status{}
	for _, entry := range entries {
		id := entry.Step.ID()
		if ctx.Err() != nil {
			blocked[id] = true
			outcome[id] = StatusSkipped
			report.Steps = append(report.Steps, skipped(id, "the run was interrupted before this step"))
			continue
		}
		if req, ok := unmetPrerequisite(entry, blocked, outcome); ok {
			blocked[id] = true
			outcome[id] = StatusSkipped
			report.Steps = append(report.Steps, skipped(id, fmt.Sprintf("a prerequisite did not pass: %s is %s", req, outcome[req])))
			continue
		}
		result := execute(ctx, entry.Step, mode)
		outcome[id] = result.Status
		report.Steps = append(report.Steps, StepReport{ID: id, Status: result.Status, Message: result.Message, Remedy: result.Remedy})
	}
	return report
}

// unmetPrerequisite names the first required step that blocks entry:
// one the runner skipped, or one that ended manual or failed. A step
// that skipped itself as not applicable does not block its dependents.
func unmetPrerequisite(entry Entry, blocked map[string]bool, outcome map[string]Status) (string, bool) {
	for _, req := range entry.Requires {
		status := outcome[req]
		if blocked[req] || status == StatusManual || status == StatusFailed {
			return req, true
		}
	}
	return "", false
}

func execute(ctx context.Context, step Step, mode Mode) Result {
	result := step.Check(ctx)
	if mode != ModeFix || result.Status != StatusManual {
		return result
	}
	if fixer, ok := step.(Fixer); ok {
		return fixer.Fix(ctx)
	}
	return result
}

func skipped(id, reason string) StepReport {
	return StepReport{ID: id, Status: StatusSkipped, Message: reason}
}
