package script

import (
	"fmt"
	"sort"
	"strings"
)

// pendingResolution is what a leaf step keeps so it can take run
// variables into its eligible text just before it runs. Only a step with
// a placeholder that neither the script env nor the CLI env resolved
// carries one.
type pendingResolution struct {
	// raw is the step as written, before any interpolation.
	raw any
	// env is the parse-time environment. It wins over a run variable of
	// the same name, matching what parsing would have done.
	env map[string]string
	// names are the placeholder names in eligible fields that env does
	// not resolve.
	names []string
}

// ResolveError reports that a step no longer validates once run
// variables are in its text. It names the step, the field and the
// placeholders, and never a value, because a value can be a credential.
type ResolveError struct {
	Command string
	Field   string
	Names   []string
}

func (e *ResolveError) Error() string {
	placeholders := make([]string, len(e.Names))
	for i, name := range e.Names {
		placeholders[i] = "${" + name + "}"
	}
	field := e.Command
	if e.Field != "" {
		field = e.Field
	}
	return fmt.Sprintf("%s: the value substituted for %s is not valid", field, strings.Join(placeholders, ", "))
}

// ResolveStep returns step with each eligible placeholder that a run
// variable supplies replaced by that variable's value, and reports
// whether anything was replaced. runVariable reports false for a name
// with no variable, which leaves its placeholder as written. A step that
// carries nothing to resolve, or whose placeholders no variable
// supplies, comes back unchanged without allocation.
//
// Each placeholder is expanded once, from the text as written: a value
// that itself holds `${name}` is inserted as plain text. The returned
// step shows the original's authored form wherever text is displayed or
// recorded, so a substituted value never leaves through a detail.
//
// A resolved step that fails validation yields a *ResolveError.
func ResolveStep(step ScriptStep, runVariable func(name string) (string, bool)) (ScriptStep, bool, error) {
	pending := step.pendingSource()
	if pending == nil {
		return step, false, nil
	}
	available := false
	for _, name := range pending.names {
		if _, ok := runVariable(name); ok {
			available = true
			break
		}
	}
	if !available {
		return step, false, nil
	}

	command := step.StepCommand()
	value, _ := stepValue(pending.raw)
	used := map[string]bool{}
	lookup := func(name string) (string, bool) {
		if v, ok := pending.env[name]; ok {
			return v, true
		}
		v, ok := runVariable(name)
		if ok {
			used[name] = true
		}
		return v, ok
	}
	base := resolveVariables(value, pending.env)
	var raw OrderedMap
	raw.Set(command, mergeRuntimeFields(command, base, value, lookup))

	resolved, err := normalizeStep(raw, 0)
	if err != nil {
		return nil, false, newResolveError(command, fieldOfMessage(err.Error()), used)
	}
	var firstProblem string
	validateStep(resolved, command, func(format string, args ...any) {
		if firstProblem == "" {
			firstProblem = fieldOfMessage(fmt.Sprintf(format, args...))
		}
	})
	if firstProblem != "" {
		return nil, false, newResolveError(command, firstProblem, used)
	}
	resolved.setAuthored(AuthoredOf(step))
	if step.writtenFormLost() {
		resolved.markWrittenFormLost()
	}
	return resolved, true, nil
}

func newResolveError(command, field string, used map[string]bool) *ResolveError {
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	return &ResolveError{Command: command, Field: field, Names: names}
}

// fieldOfMessage returns the leading field path of a validation or
// parse message, or the empty string when the message does not start
// with one. The remainder of a message can quote a value.
func fieldOfMessage(message string) string {
	head, _, found := strings.Cut(message, ": ")
	if !found || strings.ContainsAny(head, " \"'`") {
		return ""
	}
	return head
}

// stepValue returns the value under the command key of a raw step.
func stepValue(raw any) (any, string) {
	m, ok := asMap(raw)
	if !ok || len(m) == 0 {
		return nil, ""
	}
	key := firstStepKey(raw, m)
	return m[key], key
}

// attachPendingSteps pairs each step with the text it was written as,
// before interpolation, and gives every leaf step at any depth that
// holds an unresolved placeholder in an eligible field what it needs to
// resolve that placeholder at run time. Block steps are never rebuilt:
// their children resolve one by one when each runs.
func attachPendingSteps(steps []ScriptStep, rawSteps []any, env map[string]string) {
	if len(steps) != len(rawSteps) {
		return
	}
	for i, step := range steps {
		attachPending(step, rawSteps[i], env)
	}
}

func attachPending(step ScriptStep, raw any, env map[string]string) {
	value, _ := stepValue(raw)
	switch s := step.(type) {
	case *RepeatStep:
		attachPendingSteps(s.Commands, rawList(value, "commands"), env)
	case *RetryStep:
		attachPendingSteps(s.Commands, rawList(value, "commands"), env)
	case *HandleStep:
		raws := handleDoRaws(value)
		if len(raws) != len(s.Branches) {
			return
		}
		for i, do := range raws {
			attachPendingSteps(s.Branches[i].Do.Steps, asRawSteps(do), env)
		}
	case *BranchStep:
		cases := rawList(value, "on")
		if len(cases) == len(s.On) {
			for i, c := range cases {
				m, _ := asMap(c)
				attachPendingSteps(s.On[i].Do.Steps, asRawSteps(m["do"]), env)
			}
		}
		if s.Default != nil {
			m, _ := asMap(value)
			attachPendingSteps(s.Default.Steps, asRawSteps(m["default"]), env)
		}
	case *RunFlowStep:
	default:
		attachLeafPending(step, raw, value, env)
	}
}

func attachLeafPending(step ScriptStep, raw, value any, env map[string]string) {
	var names []string
	seen := map[string]bool{}
	mergeRuntimeFields(step.StepCommand(), value, value, func(name string) (string, bool) {
		if _, resolved := env[name]; !resolved && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		return "", false
	})
	if len(names) == 0 {
		return
	}
	step.setPending(&pendingResolution{raw: raw, env: env, names: names})
}

// rawList returns the sequence under key in a block step's raw value.
func rawList(value any, key string) []any {
	m, ok := asMap(value)
	if !ok {
		return nil
	}
	return asRawSteps(m[key])
}

// asRawSteps returns raw as a step sequence, or nil when it is a file
// path or absent.
func asRawSteps(raw any) []any {
	list, _ := asSlice(raw)
	return list
}

// handleDoRaws returns the `do` value of every branch of a handle step
// that normalizeHandle turns into a HandleBranch, in the same order: an
// `otherwise` entry and an entry missing `when` or `do` yield none.
func handleDoRaws(value any) []any {
	entries, _ := asSlice(value)
	var out []any
	for _, entry := range entries {
		m, ok := asMap(entry)
		if !ok {
			return nil
		}
		if _, otherwise := m["otherwise"]; otherwise {
			continue
		}
		_, hasWhen := m["when"]
		do, hasDo := m["do"]
		if hasWhen && hasDo {
			out = append(out, do)
		}
	}
	return out
}

// NeedsResolution reports whether step holds an eligible placeholder that
// a run variable could still supply. A caller checks it first so a step
// with nothing to resolve costs nothing.
func NeedsResolution(step ScriptStep) bool { return step.pendingSource() != nil }
