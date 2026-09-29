package config

import (
	"fmt"
)

// Entry point for parsing and validating an Atomyx project config
// document into a typed AtomyxConfig.

// ValidationIssue is one schema-rule violation. Path holds a mix of
// string object keys and int array indices from the document root
// down to the offending value. Message is diagnostic text for a
// Go-side caller. Message wording is not part of the config schema's
// public contract: a caller may match on Path to identify which
// field failed, but must not assert on the exact Message text, since
// that text is free to change.
type ValidationIssue struct {
	Path    []any
	Message string
}

type issueCollector struct {
	issues []ValidationIssue
}

func (c *issueCollector) add(path []any, format string, args ...any) {
	c.issues = append(c.issues, ValidationIssue{
		Path:    append([]any{}, path...),
		Message: fmt.Sprintf(format, args...),
	})
}

func withSegment(path []any, segment any) []any {
	next := make([]any, len(path)+1)
	copy(next, path)
	next[len(path)] = segment
	return next
}

// ParseAndValidate parses yamlContent as an Atomyx project config
// document and validates it against every config schema rule. It
// returns a nil config whenever any issue was collected, including
// when yamlContent is not parseable YAML, or its root is not a
// mapping, both cases reported as a single issue at the document
// root.
func ParseAndValidate(yamlContent []byte) (*AtomyxConfig, []ValidationIssue) {
	root, environmentOrder, ok := decodeYAMLDocument(yamlContent)
	if !ok {
		return nil, []ValidationIssue{{Path: []any{}, Message: "expected a YAML mapping"}}
	}
	return ValidateDocument(root, environmentOrder)
}

var topLevelKeys = map[string]bool{
	"project": true, "apps": true, "environments": true, "launch": true,
	"preconditions": true, "report": true, "suites": true,
	"testRoots": true, "dataRoots": true,
}

// ValidateDocument checks a generically decoded YAML document against
// every rule AtomyxConfigSchema enforces and, when the document has
// no violation, builds the typed AtomyxConfig. environmentOrder
// carries the source declaration order of the top-level
// "environments" mapping's keys (see decodeYAMLDocument) into the
// built config's EnvironmentOrder field.
func ValidateDocument(root map[string]any, environmentOrder []string) (*AtomyxConfig, []ValidationIssue) {
	c := &issueCollector{}
	path := []any{}

	checkUnknownKeys(root, topLevelKeys, path, c)

	project, _ := requiredString(root, "project", 1, path, c)
	apps := validateApps(root, path, c)
	environments := validateEnvironments(root, path, c)
	launch := validateLaunchPolicy(root, path, c)
	preconditions := validateEnum(root, "preconditions", []string{PreconditionsSkip, PreconditionsForce}, path, c)
	report := validateReport(root, path, c)
	suites := validateSuites(root, path, c)
	testRoots := optionalStringArray(root, "testRoots", 1, path, c)
	dataRoots := optionalStringArray(root, "dataRoots", 1, path, c)

	// Matches zod's own superRefine semantics: the cross-field check
	// only runs once the base object schema has produced zero issues,
	// the same way AtomyxConfigSchema's superRefine callback never
	// runs on a document that already failed a field-level rule.
	if len(c.issues) == 0 {
		checkEnvironmentAppReferences(*apps, *environments, path, c)
	}

	if len(c.issues) > 0 {
		return nil, c.issues
	}

	return &AtomyxConfig{
		Project:          project,
		Apps:             *apps,
		Environments:     *environments,
		EnvironmentOrder: environmentOrder,
		Launch:           launch,
		Preconditions:    preconditions,
		Report:           report,
		Suites:           suites,
		TestRoots:        testRoots,
		DataRoots:        dataRoots,
	}, nil
}
