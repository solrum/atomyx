package inspector

import (
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
)

// scriptSelector converts a selector to the form a script step holds.
// A script selector matches id, text, label, hint and role by exact
// string and takes an nth, so a selector that uses a regular
// expression, a value, or an enabled, clickable or focused constraint
// has no script form. The error it returns is coded INVALID_ARGUMENT
// and names the field that cannot be written.
func scriptSelector(sel orchestra.Selector) (script.ScriptSelector, error) {
	var out script.ScriptSelector
	var failed error
	literal := func(field string, p *orchestra.Pattern) *string {
		if p == nil {
			return nil
		}
		s, ok := p.Literal()
		if !ok {
			failed = invalidArgument("the "+field+" is a regular expression, which a script step cannot hold",
				"Use an exact string for the "+field+".")
			return nil
		}
		return &s
	}
	out.ID = literal("id", sel.ID)
	out.Text = literal("text", sel.Text)
	out.Label = literal("label", sel.Label)
	out.Hint = literal("hint", sel.Hint)
	if failed != nil {
		return script.ScriptSelector{}, failed
	}
	switch {
	case sel.Value != nil:
		return script.ScriptSelector{}, invalidArgument("a script step cannot match on a value",
			"Use id, text, label, hint, role or nth.")
	case sel.Enabled != nil || sel.Clickable != nil || sel.Focused != nil:
		return script.ScriptSelector{}, invalidArgument("a script step cannot constrain enabled, clickable or focused",
			"Use id, text, label, hint, role or nth.")
	}
	out.Role = sel.Role
	out.Nth = sel.Nth
	return out, nil
}
