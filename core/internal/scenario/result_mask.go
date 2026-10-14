package scenario

import "dev.atomyx/internal/runner"

// Masked returns a copy of the result whose free text fields pass through
// mask. The receiver is left as it was.
func (r ScenarioResult) Masked(mask func(string) string) ScenarioResult {
	out := r
	out.Scripts = make([]ScenarioScriptResult, len(r.Scripts))
	for i, child := range r.Scripts {
		child.Error = runner.MaskedText(child.Error, mask)
		if child.Result != nil {
			masked := child.Result.Masked(mask)
			child.Result = &masked
		}
		out.Scripts[i] = child
	}
	return out
}

func (o *SuiteEntryOutcome) masked(mask func(string) string) *SuiteEntryOutcome {
	if o == nil {
		return nil
	}
	out := &SuiteEntryOutcome{}
	if o.Script != nil {
		script := o.Script.Masked(mask)
		out.Script = &script
	}
	if o.Scenario != nil {
		scenario := o.Scenario.Masked(mask)
		out.Scenario = &scenario
	}
	return out
}

// Masked returns a copy of the entry whose free text fields, including
// those of every attempt, pass through mask. The receiver is left as it was.
func (e SuiteEntryResult) Masked(mask func(string) string) SuiteEntryResult {
	out := e
	out.Error = runner.MaskedText(e.Error, mask)
	out.Result = e.Result.masked(mask)
	if e.Attempts != nil {
		out.Attempts = make([]SuiteAttempt, len(e.Attempts))
		for i, attempt := range e.Attempts {
			attempt.Error = runner.MaskedText(attempt.Error, mask)
			attempt.Failure = mask(attempt.Failure)
			attempt.Result = attempt.Result.masked(mask)
			out.Attempts[i] = attempt
		}
	}
	return out
}

// Masked returns a copy of the suite result whose entries pass through
// mask. The receiver is left as it was.
func (r SuiteResult) Masked(mask func(string) string) SuiteResult {
	out := r
	out.Entries = make([]SuiteEntryResult, len(r.Entries))
	for i, entry := range r.Entries {
		out.Entries[i] = entry.Masked(mask)
	}
	return out
}
