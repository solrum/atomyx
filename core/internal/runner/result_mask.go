package runner

// Masked returns a copy of the result whose free text fields, the step
// details and the abort message and remedy, pass through mask. The
// receiver is left as it was.
func (r ScriptResult) Masked(mask func(string) string) ScriptResult {
	out := r
	out.Steps = make([]StepResult, len(r.Steps))
	for i, step := range r.Steps {
		step.Detail = MaskedText(step.Detail, mask)
		out.Steps[i] = step
	}
	if r.Abort != nil {
		abort := *r.Abort
		abort.Message = mask(abort.Message)
		abort.Remedy = mask(abort.Remedy)
		out.Abort = &abort
	}
	return out
}

// MaskedText returns text passed through mask, or nil for nil.
func MaskedText(text *string, mask func(string) string) *string {
	if text == nil {
		return nil
	}
	masked := mask(*text)
	return &masked
}
