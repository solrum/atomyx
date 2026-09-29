package runner

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// AssertApiCommand asserts that the request captured under a name
// matches an expected status and/or a set of dot-path body
// assertions.
type AssertApiCommand struct{}

func (AssertApiCommand) Command() string { return script.CommandAssertApi }

func (AssertApiCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.AssertApiStep)
	fromKey := unwrapFromKey(s.From)
	captured, ok := ctx.Captures[fromKey]
	if !ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"No captured request named \"%s\". Did you run a \"capture\" step with \"as: %s\" before this assertion?",
			s.From, s.From))}
	}

	var failures []string

	if s.Status != nil && captured.Status != *s.Status {
		failures = append(failures, fmt.Sprintf("status: expected %d, got %d", *s.Status, captured.Status))
	}

	if s.Body != nil {
		for _, path := range s.Body.Keys() {
			expected, _ := s.Body.Get(path)
			actual := resolveDotPath(captured.Body, path)
			outcome := assertValue(actual, expected)
			if !outcome.ok {
				failures = append(failures, fmt.Sprintf("body %s: %s", path, outcome.reason))
			}
		}
	}

	if len(failures) > 0 {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"API assertion failed on \"%s\":\n  - %s", s.From, strings.Join(failures, "\n  - ")))}
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
		"API assertion passed for \"%s\" (%s %s)", s.From, captured.Method, captured.URL))}
}

// assertOutcome is the result of matching one actual value against
// one expected value or operator in an assertApi body assertion.
type assertOutcome struct {
	ok     bool
	reason string
}

var (
	assertContainsPattern   = regexp.MustCompile(`^contains:(.+)$`)
	assertBetweenPattern    = regexp.MustCompile(`^between:([^,]+),(.+)$`)
	assertComparisonPattern = regexp.MustCompile(`^(gt|gte|lt|lte):(.+)$`)
)

// assertValue checks actual against expected. A string expected value
// prefixed with "$" selects an operator (not_empty, exists,
// not_exists, contains, between, or a gt/gte/lt/lte comparison);
// every other expected value is compared by exact deep equality.
// The operator vocabulary and the exact wording of each failure
// reason are a stable contract: a script author's assertion failure
// message depends on it staying the same across runs.
func assertValue(actual, expected any) assertOutcome {
	if expectedStr, isStr := expected.(string); isStr && strings.HasPrefix(expectedStr, "$") {
		op := expectedStr[1:]

		switch op {
		case "not_empty":
			if isJSUndefined(actual) || actual == nil || actual == "" || isEmptyArray(actual) {
				return assertOutcome{false, fmt.Sprintf("expected $not_empty, got %s", jsonStringifyJS(actual))}
			}
			return assertOutcome{true, ""}
		case "exists":
			if isJSUndefined(actual) {
				return assertOutcome{false, "expected $exists, got undefined"}
			}
			return assertOutcome{true, ""}
		case "not_exists":
			if !isJSUndefined(actual) {
				return assertOutcome{false, fmt.Sprintf("expected $not_exists, got %s", jsonStringifyJS(actual))}
			}
			return assertOutcome{true, ""}
		}

		if m := assertContainsPattern.FindStringSubmatch(op); m != nil {
			needle := m[1]
			s, isStr := actual.(string)
			if !isStr || !strings.Contains(s, needle) {
				return assertOutcome{false, fmt.Sprintf("expected $contains:\"%s\", got %s", needle, jsonStringifyJS(actual))}
			}
			return assertOutcome{true, ""}
		}

		if m := assertBetweenPattern.FindStringSubmatch(op); m != nil {
			min, minErr := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
			max, maxErr := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
			num, numErr := toJSNumber(actual)
			if minErr != nil || maxErr != nil || numErr != nil {
				return assertOutcome{false, fmt.Sprintf(
					"cannot compare: actual=%s, range=%s,%s", jsonStringifyJS(actual), m[1], m[2])}
			}
			if num < min || num > max {
				return assertOutcome{false, fmt.Sprintf(
					"expected $between:%s,%s, got %s", expression.FormatNumber(min), expression.FormatNumber(max), expression.FormatNumber(num))}
			}
			return assertOutcome{true, ""}
		}

		if m := assertComparisonPattern.FindStringSubmatch(op); m != nil {
			cmp := m[1]
			threshold, threshErr := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
			num, numErr := toJSNumber(actual)
			if threshErr != nil || numErr != nil {
				return assertOutcome{false, fmt.Sprintf(
					"cannot compare: actual=%s, threshold=%s", jsonStringifyJS(actual), m[2])}
			}
			var passed bool
			switch cmp {
			case "gt":
				passed = num > threshold
			case "gte":
				passed = num >= threshold
			case "lt":
				passed = num < threshold
			default: // "lte"
				passed = num <= threshold
			}
			if !passed {
				return assertOutcome{false, fmt.Sprintf("expected $%s:%s, got %s", cmp, expression.FormatNumber(threshold), expression.FormatNumber(num))}
			}
			return assertOutcome{true, ""}
		}

		// An unrecognized "$"-prefixed operator falls through to exact
		// match, treating the "$" as a literal character in the
		// expected string rather than a rejected input.
	}

	if !jsDeepEqual(actual, expected) {
		return assertOutcome{false, fmt.Sprintf("expected %s, got %s", jsonStringifyJS(expected), jsonStringifyJS(actual))}
	}
	return assertOutcome{true, ""}
}
