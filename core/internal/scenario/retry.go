package scenario

// MaxRetries is the most retries an entry may be given after its first
// attempt.
const MaxRetries = 3

// clampRetries limits a requested retry count to 0 through MaxRetries.
func clampRetries(retries int) int {
	return min(max(retries, 0), MaxRetries)
}

// retryable reports whether a failed attempt may be run again. Only a
// test failure is: a step that failed, or a scenario script that failed.
// Anything else would fail the same way again or hide a real fault, so it
// is final:
//
//   - a canceled or interrupted suite context
//   - an entry that could not be loaded or parsed, or a scenario script
//     that could not
//   - a run that aborted early, such as a lost device
func retryable(ctxErr error, attempt SuiteAttempt) bool {
	if ctxErr != nil || attempt.Status != EntryFailed || attempt.Error != nil || attempt.Result == nil {
		return false
	}
	switch {
	case attempt.Result.Script != nil:
		return attempt.Result.Script.Abort == nil
	case attempt.Result.Scenario != nil:
		for _, child := range attempt.Result.Scenario.Scripts {
			if child.Status == StatusErrored || (child.Result != nil && child.Result.Abort != nil) {
				return false
			}
		}
		return true
	}
	return false
}
