package script

import "fmt"

// Validation of the block steps that run a nested command list
// repeatedly.

func validateCommandList(commands []ScriptStep, path string, report reportFunc) {
	if len(commands) == 0 {
		report("%s: must have at least one step", path)
	}
	for i, c := range commands {
		validateStep(c, fmt.Sprintf("%s[%d]", path, i), report)
	}
}

func validateRepeatStep(s *RepeatStep, path string, report reportFunc) {
	if s.Times == nil && s.While == nil {
		report("%s: at least one of `times` or `while` must be set", path)
	}
	if s.Times != nil && *s.Times < 1 {
		report("%s.times: must be at least 1, got %d", path, *s.Times)
	}
	if s.While != nil {
		validateRepeatCondition(s.While, path+".while", report)
	}
	validateCommandList(s.Commands, path+".commands", report)
}

func validateRepeatCondition(c *RepeatCondition, path string, report reportFunc) {
	set := 0
	if c.Visible != nil {
		set++
	}
	if c.NotVisible != nil {
		set++
	}
	if c.Expr != nil {
		set++
	}
	if set != 1 {
		report("%s: exactly one of `visible`, `notVisible` or `expr` must be set, got %d", path, set)
	}
	validateStringOrSelector(c.Visible, path+".visible", report)
	validateStringOrSelector(c.NotVisible, path+".notVisible", report)
	if c.Expr != nil && len(*c.Expr) == 0 {
		report("%s.expr: must not be empty", path)
	}
}

func validateRetryStep(s *RetryStep, path string, report reportFunc) {
	if s.MaxRetries != nil && (*s.MaxRetries < 0 || *s.MaxRetries > RetryMaxRetriesLimit) {
		report("%s.maxRetries: must be between 0 and %d, got %d", path, RetryMaxRetriesLimit, *s.MaxRetries)
	}
	validateCommandList(s.Commands, path+".commands", report)
}
