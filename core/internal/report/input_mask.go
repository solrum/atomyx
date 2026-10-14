package report

// maskedInput returns input with every free text field of its result and
// of its abort metadata passed through mask. Any other input kind is
// returned as it is.
func maskedInput(input ReportInput, mask func(string) string) ReportInput {
	switch typed := input.(type) {
	case ScriptReportInput:
		typed.Metadata = maskedMetadata(typed.Metadata, mask)
		typed.Result = typed.Result.Masked(mask)
		return typed
	case ScenarioReportInput:
		typed.Metadata = maskedMetadata(typed.Metadata, mask)
		typed.Result = typed.Result.Masked(mask)
		return typed
	case SuiteReportInput:
		typed.Metadata = maskedMetadata(typed.Metadata, mask)
		typed.Result = typed.Result.Masked(mask)
		return typed
	}
	return input
}

func maskedMetadata(metadata ReportMetadata, mask func(string) string) ReportMetadata {
	if metadata.Abort != nil {
		abort := *metadata.Abort
		abort.Message = mask(abort.Message)
		abort.Remedy = mask(abort.Remedy)
		metadata.Abort = &abort
	}
	return metadata
}
