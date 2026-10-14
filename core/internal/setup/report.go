package setup

// StepReport is the outcome of one step as it is printed.
type StepReport struct {
	ID      string `json:"id"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Remedy  string `json:"remedy"`
}

// Report is the outcome of a whole run, one entry per registered step
// in run order.
type Report struct {
	Steps []StepReport
}

// OK reports whether every step ended satisfied, that is ok or fixed.
// A skipped, manual or failed step makes the run not ok.
func (r Report) OK() bool {
	for _, s := range r.Steps {
		if !s.Status.Satisfied() {
			return false
		}
	}
	return true
}

// ExitCode is the process exit code for the report: 0 when OK, 1
// otherwise.
func (r Report) ExitCode() int {
	if r.OK() {
		return 0
	}
	return 1
}
