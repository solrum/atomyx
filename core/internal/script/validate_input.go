package script

import "slices"

func validateEraseTextStep(s *EraseTextStep, path string, report reportFunc) {
	if s.Characters != nil && *s.Characters < 1 {
		report("%s.charactersToErase: must be at least 1", path)
	}
}

func validateScrollUntilVisibleStep(s *ScrollUntilVisibleStep, path string, report reportFunc) {
	validateSelector(&s.Selector, path+".selector", report)
	if s.Direction != nil && *s.Direction != "up" && *s.Direction != "down" {
		report("%s.direction: must be one of up/down, got %q", path, *s.Direction)
	}
}

func validateInputRandomStep(s *InputRandomStep, path string, report reportFunc) {
	if !slices.Contains(RandomKinds, s.Kind) {
		report("%s.kind: must be one of text/number/email/personName, got %q", path, s.Kind)
	}
	if s.Length == nil {
		return
	}
	if s.Kind == RandomKindPersonName {
		report("%s.length: does not apply to personName", path)
	}
	if *s.Length < 1 || *s.Length > RandomLengthLimit {
		report("%s.length: must be between 1 and %d", path, RandomLengthLimit)
	}
}
