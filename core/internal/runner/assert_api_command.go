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
	written := authored[*script.AssertApiStep](step)
	fromKey := unwrapFromKey(s.From)
	captured, ok := ctx.Captures[fromKey]
	if !ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"No captured request named \"%s\". Did you run a \"capture\" step with \"as: %s\" before this assertion?",
			written.From, written.From))}
	}

	var failures []string

	if s.Status != nil && captured.Status != *s.Status {
		failures = append(failures, fmt.Sprintf("status: expected %d, got %d", *s.Status, captured.Status))
	}

	if s.Body != nil {
		for _, path := range s.Body.Keys() {
			expected, _ := s.Body.Get(path)
			expectedAsWritten, _ := written.Body.Get(path)
			actual := resolveDotPath(captured.Body, path)
			outcome := assertValue(actual, expected, expectedAsWritten)
			if !outcome.ok {
				failures = append(failures, fmt.Sprintf("body %s: %s", path, outcome.reason))
			}
		}
	}

	if len(failures) > 0 {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"API assertion failed on \"%s\":\n  - %s", written.From, strings.Join(failures, "\n  - ")))}
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
		"API assertion passed for \"%s\" (%s %s)", written.From, captured.Method, captured.URL))}
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
// written is expected as the script author wrote it, before `${var}`
// placeholders were expanded, and every failure reason quotes it in place
// of expected. The operator vocabulary and the exact wording of each
// failure reason are a stable contract: a script author's assertion
// failure message depends on it staying the same across runs.
func assertValue(actual, expected, written any) assertOutcome {
	if expectedStr, isStr := expected.(string); isStr && strings.HasPrefix(expectedStr, "$") {
		op := expectedStr[1:]
		writtenOp := op
		if writtenStr, ok := written.(string); ok && strings.HasPrefix(writtenStr, "$") {
			writtenOp = writtenStr[1:]
		}
		// A threshold prints in its parsed numeric form unless the author
		// wrote a placeholder, in which case it prints as written.
		shownNumber := func(parsed float64, raw string) string {
			if writtenOp == op {
				return expression.FormatNumber(parsed)
			}
			return strings.TrimSpace(raw)
		}
		unreadable := func(got string) assertOutcome {
			return assertOutcome{false, fmt.Sprintf("expected $%s, got %s", writtenOp, got)}
		}

		switch op {
		case "not_empty":
			if isJSUndefined(actual) || actual == nil || actual == "" || isEmptyArray(actual) {
				return unreadable(describeCapturedValue(actual))
			}
			return assertOutcome{true, ""}
		case "exists":
			if isJSUndefined(actual) {
				return unreadable("undefined")
			}
			return assertOutcome{true, ""}
		case "not_exists":
			if !isJSUndefined(actual) {
				return unreadable(describeCapturedValue(actual))
			}
			return assertOutcome{true, ""}
		}

		if m := assertContainsPattern.FindStringSubmatch(op); m != nil {
			needle := m[1]
			s, isStr := actual.(string)
			if !isStr || !strings.Contains(s, needle) {
				if w := assertContainsPattern.FindStringSubmatch(writtenOp); w != nil {
					return assertOutcome{false, fmt.Sprintf("expected $contains:\"%s\", got %s", w[1], describeCapturedValue(actual))}
				}
				return unreadable(describeCapturedValue(actual))
			}
			return assertOutcome{true, ""}
		}

		if m := assertBetweenPattern.FindStringSubmatch(op); m != nil {
			min, minErr := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
			max, maxErr := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
			num, numErr := toJSNumber(actual)
			w := assertBetweenPattern.FindStringSubmatch(writtenOp)
			if minErr != nil || maxErr != nil || numErr != nil {
				if w == nil {
					return unreadable(describeCapturedValue(actual))
				}
				return assertOutcome{false, fmt.Sprintf(
					"cannot compare: actual=%s, range=%s,%s", describeCapturedValue(actual), w[1], w[2])}
			}
			if num < min || num > max {
				if w == nil {
					return unreadable("a number outside that range")
				}
				return assertOutcome{false, fmt.Sprintf(
					"expected $between:%s,%s, got a number outside that range", shownNumber(min, w[1]), shownNumber(max, w[2]))}
			}
			return assertOutcome{true, ""}
		}

		if m := assertComparisonPattern.FindStringSubmatch(op); m != nil {
			cmp := m[1]
			threshold, threshErr := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
			num, numErr := toJSNumber(actual)
			w := assertComparisonPattern.FindStringSubmatch(writtenOp)
			if threshErr != nil || numErr != nil {
				if w == nil {
					return unreadable(describeCapturedValue(actual))
				}
				return assertOutcome{false, fmt.Sprintf(
					"cannot compare: actual=%s, threshold=%s", describeCapturedValue(actual), w[2])}
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
				if w == nil {
					return unreadable("a number that does not satisfy it")
				}
				return assertOutcome{false, fmt.Sprintf("expected $%s:%s, got a number that does not satisfy it", w[1], shownNumber(threshold, w[2]))}
			}
			return assertOutcome{true, ""}
		}

	}

	if !jsDeepEqual(actual, expected) {
		return assertOutcome{false, fmt.Sprintf("expected %s, got %s", jsonStringifyJS(written), describeCapturedValue(actual))}
	}
	return assertOutcome{true, ""}
}
