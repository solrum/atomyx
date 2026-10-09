package report

import (
	"encoding/xml"
	"fmt"
	"strconv"

	"dev.atomyx/internal/scenario"
)

// defaultJUnitSuiteName names the testsuite and the classname of a suite
// run that has no suite name of its own, such as an ad-hoc tag selection.
const defaultJUnitSuiteName = "suite"

// quarantinedMessage is the message of the skipped element a quarantined
// failure is written as.
const quarantinedMessage = "quarantined"

type junitSuite struct {
	XMLName   xml.Name    `xml:"testsuite"`
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Errors    int         `xml:"errors,attr"`
	Skipped   int         `xml:"skipped,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr,omitempty"`
	Cases     []junitCase `xml:"testcase"`
}

type junitCase struct {
	Classname string          `xml:"classname,attr"`
	Name      string          `xml:"name,attr"`
	Time      string          `xml:"time,attr"`
	Failure   *junitProblem   `xml:"failure"`
	Error     *junitProblem   `xml:"error"`
	Skipped   *junitSkipped   `xml:"skipped"`
	Flaky     []junitFlakyRun `xml:"flakyFailure"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// junitFlakyRun is the Surefire flakyFailure element: one failed attempt
// of a test case that passed on a later attempt.
type junitFlakyRun struct {
	Message string `xml:"message,attr"`
	Time    string `xml:"time,attr"`
	Body    string `xml:",chardata"`
}

// RenderJUnit renders the JUnit XML report of input. A script or scenario
// run becomes one testsuite holding one testcase. A suite becomes one
// testsuite holding one testcase per entry, with the suite name as the
// classname and the entry path as the name.
//
// The counts agree with the run's exit code: failures and errors together
// are the entries and runs that fail it, and skipped counts quarantined
// failures, which do not. An entry that failed and then passed is a passing
// testcase carrying one flakyFailure element per failed attempt.
func RenderJUnit(input ReportInput) ([]byte, error) {
	var suite junitSuite
	switch v := input.(type) {
	case ScriptReportInput:
		suite = singleRunSuite(v.Metadata, v.Result.ScriptName, v.Result.DurationMs, v.Result.Ok, scriptFailureText(v.Result))
	case ScenarioReportInput:
		suite = singleRunSuite(v.Metadata, v.Result.ScenarioName, v.Result.DurationMs, v.Result.Ok, scenarioFailureText(v.Result))
	case SuiteReportInput:
		suite = suiteSuite(v.Metadata, v.Result)
	default:
		return nil, fmt.Errorf("report: unsupported report kind %q", input.ReportKind())
	}
	encoded, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: encoding junit report: %w", err)
	}
	return append([]byte(xml.Header), append(encoded, '\n')...), nil
}

// singleRunSuite builds the testsuite of a standalone script or scenario
// run. A failed run carries a failure element with failureText.
func singleRunSuite(metadata ReportMetadata, name string, durationMs int64, ok bool, failureText string) junitSuite {
	testCase := junitCase{Classname: name, Name: name, Time: junitSeconds(durationMs)}
	suite := junitSuite{
		Name:      name,
		Tests:     1,
		Time:      junitSeconds(durationMs),
		Timestamp: metadata.StartedAt,
	}
	if !ok {
		testCase.Failure = &junitProblem{Message: failureText, Body: failureText}
		suite.Failures = 1
	}
	suite.Cases = []junitCase{testCase}
	return suite
}

// suiteSuite builds the testsuite of a suite run from the same attempts
// the suite index lists.
func suiteSuite(metadata ReportMetadata, result scenario.SuiteResult) junitSuite {
	name := defaultJUnitSuiteName
	if result.SuiteName != nil {
		name = *result.SuiteName
	}
	suite := junitSuite{
		Name:      name,
		Tests:     len(result.Entries),
		Time:      junitSeconds(result.DurationMs),
		Timestamp: metadata.StartedAt,
		Cases:     make([]junitCase, len(result.Entries)),
	}
	for i, entry := range result.Entries {
		testCase := junitCase{Classname: name, Name: entry.Path, Time: junitSeconds(entry.DurationMs)}
		attempts := entryAttempts(entry)
		last := attempts[len(attempts)-1]
		switch {
		case entry.Status == scenario.EntryPassed:
			for _, attempt := range attempts {
				if attempt.Status == scenario.EntryPassed {
					continue
				}
				text, _ := AttemptFailureText(attempt)
				testCase.Flaky = append(testCase.Flaky, junitFlakyRun{Message: text, Time: junitSeconds(attempt.DurationMs), Body: text})
			}
		case entry.QuarantinedFailure():
			testCase.Skipped = &junitSkipped{Message: quarantinedMessage}
			suite.Skipped++
		default:
			text, ran := AttemptFailureText(last)
			problem := &junitProblem{Message: text, Body: text}
			if ran {
				testCase.Failure = problem
				suite.Failures++
			} else {
				testCase.Error = problem
				suite.Errors++
			}
		}
		suite.Cases[i] = testCase
	}
	return suite
}

// entryAttempts lists the attempts of an entry, treating an entry built
// without attempts as one attempt that carries the entry's own outcome.
func entryAttempts(entry scenario.SuiteEntryResult) []scenario.SuiteAttempt {
	if len(entry.Attempts) > 0 {
		return entry.Attempts
	}
	return []scenario.SuiteAttempt{{
		Number:     1,
		Status:     entry.Status,
		Error:      entry.Error,
		DurationMs: entry.DurationMs,
		Result:     entry.Result,
	}}
}

// junitSeconds renders a duration in milliseconds as the seconds JUnit
// expects.
func junitSeconds(durationMs int64) string {
	return strconv.FormatFloat(float64(durationMs)/1000, 'f', 3, 64)
}
