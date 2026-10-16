package runner

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/script"
)

// BreakLocation addresses one authored top-level step: a script source
// and a 1-based line. Source is an absolute file path, or "inline" for
// a script that has no file.
type BreakLocation struct {
	Source string
	Line   int
}

// String renders the location in the form a GateStep reports, so a
// gate can match a breakpoint by comparing strings.
func (l BreakLocation) String() string {
	return l.Source + ":" + strconv.Itoa(l.Line)
}

// BreakScript is one script a run executes, with the path it was read
// from. An empty Path marks an inline script.
type BreakScript struct {
	Path       string
	Definition *script.ScriptDefinition
}

// ParseBreakLocation parses "<file>:<line>" or "inline:<line>". A
// relative file is resolved against baseDir, and the result is cleaned
// to an absolute path so it compares equal to the source path the
// runner reports. A malformed spec returns an INVALID_ARGUMENT error.
func ParseBreakLocation(spec, baseDir string) (BreakLocation, error) {
	cut := strings.LastIndex(spec, ":")
	if cut < 0 {
		return BreakLocation{}, invalidBreak(fmt.Sprintf("Break location %q must look like <file>:<line> or inline:<line>", spec))
	}
	file, lineText := spec[:cut], spec[cut+1:]
	line, err := strconv.Atoi(lineText)
	if err != nil || line < 1 {
		return BreakLocation{}, invalidBreak(fmt.Sprintf("Break location %q needs a line number of 1 or more", spec))
	}
	if file == "" {
		return BreakLocation{}, invalidBreak(fmt.Sprintf("Break location %q needs a file or the word inline", spec))
	}
	if file == inlineSource {
		return BreakLocation{Source: inlineSource, Line: line}, nil
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(baseDir, file)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return BreakLocation{}, invalidBreak(fmt.Sprintf("Break location %q has an unusable file path", spec))
	}
	return BreakLocation{Source: abs, Line: line}, nil
}

// ValidateBreakLocations checks every location against the authored
// top-level steps of scripts. It returns an INVALID_ARGUMENT error for
// the first location whose file is not one of the scripts, whose line
// starts no top-level step, or whose line lies inside a nested step
// list.
func ValidateBreakLocations(locations []BreakLocation, scripts []BreakScript) error {
	for _, loc := range locations {
		if err := validateBreakLocation(loc, scripts); err != nil {
			return err
		}
	}
	return nil
}

func validateBreakLocation(loc BreakLocation, scripts []BreakScript) error {
	known := false
	nested := false
	for _, s := range scripts {
		if breakSource(s.Path) != loc.Source {
			continue
		}
		known = true
		switch classifyBreakLine(s.Definition, loc.Line) {
		case breakTopLevel:
			return nil
		case breakNested:
			nested = true
		}
	}
	switch {
	case !known:
		return invalidBreak(fmt.Sprintf("Break location %s: no script of this run is at %s", loc, loc.Source))
	case nested:
		return invalidBreak(fmt.Sprintf("Break location %s is inside a nested step list. Nested steps are reached by stepping", loc))
	default:
		return invalidBreak(fmt.Sprintf("Break location %s: no top-level step starts at line %d", loc, loc.Line))
	}
}

func breakSource(path string) string {
	if path == "" {
		return inlineSource
	}
	return filepath.Clean(path)
}

type breakLineKind int

const (
	breakNone breakLineKind = iota
	breakTopLevel
	breakNested
)

// classifyBreakLine reports whether line starts a top-level step of
// def, or falls between a nesting step's own line and the next
// top-level step. Nested steps carry no line data, so the span after a
// nesting step is the only evidence that a line belongs to one.
func classifyBreakLine(def *script.ScriptDefinition, line int) breakLineKind {
	for i := range def.Steps {
		start := lineForStep(def, i)
		if start == nil {
			continue
		}
		if *start == line {
			return breakTopLevel
		}
		if *start > line || !isNestingStep(def.Steps[i]) {
			continue
		}
		if next := nextStepLine(def, i); next == 0 || line < next {
			return breakNested
		}
	}
	return breakNone
}

// nextStepLine returns the line of the first top-level step after i
// that has one, or 0 when there is none.
func nextStepLine(def *script.ScriptDefinition, i int) int {
	for j := i + 1; j < len(def.Steps); j++ {
		if l := lineForStep(def, j); l != nil {
			return *l
		}
	}
	return 0
}

func isNestingStep(step script.ScriptStep) bool {
	switch step.(type) {
	case *script.RunFlowStep, *script.HandleStep, *script.BranchStep, *script.RepeatStep, *script.RetryStep:
		return true
	}
	return false
}

func invalidBreak(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
