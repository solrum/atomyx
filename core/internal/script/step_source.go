package script

import "reflect"

// stepSource is embedded in every step. It holds the step as the script
// author wrote it, which differs from the step itself only when `${var}`
// interpolation changed some of its text.
type stepSource struct {
	authored ScriptStep
	lost     bool
}

func (s *stepSource) authoredStep() ScriptStep { return s.authored }
func (s *stepSource) setAuthored(a ScriptStep) { s.authored = a }
func (s *stepSource) writtenFormLost() bool    { return s.lost }
func (s *stepSource) markWrittenFormLost()     { s.lost = true }

// WrittenFormLost reports whether step carries text a `${var}`
// placeholder expanded and its written form could not be rebuilt. Such a
// step must be shown by its command name alone, because every field it
// holds may be an expanded value. A step with no placeholder, or whose
// written form AuthoredOf returns, is never lost.
func WrittenFormLost(step ScriptStep) bool { return step.writtenFormLost() }

// markWrittenFormLost flags step and every inline step nested in it.
func markWrittenFormLost(step ScriptStep) {
	step.markWrittenFormLost()
	switch s := step.(type) {
	case *HandleStep:
		for _, b := range s.Branches {
			markListLost(b.Do)
		}
	case *BranchStep:
		for _, c := range s.On {
			markListLost(c.Do)
		}
		if s.Default != nil {
			markListLost(*s.Default)
		}
	case *RepeatStep:
		markListLost(StepsOrFile{Steps: s.Commands})
	}
}

func markListLost(list StepsOrFile) {
	for _, nested := range list.Steps {
		markWrittenFormLost(nested)
	}
}

// AuthoredOf returns step as the script author wrote it, with `${var}`
// placeholders still unexpanded. A step that no placeholder touched, or
// that was built without parsing a script, is its own authored form.
// Text shown to people or recorded in a run folder comes from this form,
// so a value a variable carries never leaves the process through it.
func AuthoredOf(step ScriptStep) ScriptStep {
	if authored := step.authoredStep(); authored != nil {
		return authored
	}
	return step
}

// attachAuthored records authored as the written form of resolved when
// interpolation changed it, including any inline steps nested in a
// handle, branch or block step. It does nothing for steps of different kinds or
// steps that are identical.
func attachAuthored(resolved, authored ScriptStep) {
	if reflect.TypeOf(resolved) != reflect.TypeOf(authored) || reflect.DeepEqual(resolved, authored) {
		return
	}
	switch r := resolved.(type) {
	case *HandleStep:
		a := authored.(*HandleStep)
		for i := range min(len(r.Branches), len(a.Branches)) {
			attachAuthoredList(r.Branches[i].Do, a.Branches[i].Do)
		}
	case *BranchStep:
		a := authored.(*BranchStep)
		for i := range min(len(r.On), len(a.On)) {
			attachAuthoredList(r.On[i].Do, a.On[i].Do)
		}
		if r.Default != nil && a.Default != nil {
			attachAuthoredList(*r.Default, *a.Default)
		}
	case *RepeatStep:
		a := authored.(*RepeatStep)
		attachAuthoredList(StepsOrFile{Steps: r.Commands}, StepsOrFile{Steps: a.Commands})
	}
	resolved.setAuthored(authored)
}

func attachAuthoredList(resolved, authored StepsOrFile) {
	for i := range min(len(resolved.Steps), len(authored.Steps)) {
		attachAuthored(resolved.Steps[i], authored.Steps[i])
	}
}
