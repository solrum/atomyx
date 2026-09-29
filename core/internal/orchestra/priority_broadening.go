package orchestra

import "sort"

// CompileSelector compiles a Selector into an ElementFilter using
// Atomyx's priority-broadening policy: try each content field the
// caller provided, in a fixed priority order, and return the first
// field that resolves to a non-empty match.
//
// Priority order (most specific to most fuzzy): ID, Label, Text,
// Value, Hint. When the caller provides Text but not Label, Text's
// match attempt is followed by a Label match attempt against the
// same pattern, because a cross-platform Text query against an
// element that only carries an accessibility label needs a fallback
// to match.
//
// Role, Enabled, Clickable, and Focused are not part of the priority
// chain: they are AND-ed onto every content field's filter as
// additional constraints. When the caller provides no content field,
// the result is constraint-only, matching every cursor that
// satisfies the constraints.
//
// Nth applies after broadening, selecting one match from the winning
// filter's result list by position; when Nth is set, ranking is
// skipped because Nth is positional by definition.
func CompileSelector(s Selector) ElementFilter {
	var constraints []ElementFilter
	if s.Role != nil {
		constraints = append(constraints, RoleIs(*s.Role))
	}
	if s.Enabled != nil && *s.Enabled {
		constraints = append(constraints, IsEnabled())
	}
	if s.Clickable != nil && *s.Clickable {
		constraints = append(constraints, IsClickable())
	}
	if s.Focused != nil && *s.Focused {
		constraints = append(constraints, IsFocused())
	}

	var priorityFilters []ElementFilter
	if s.ID != nil {
		priorityFilters = append(priorityFilters, IDMatches(*s.ID))
	}
	if s.Label != nil {
		priorityFilters = append(priorityFilters, LabelMatches(*s.Label))
	}
	if s.Text != nil {
		priorityFilters = append(priorityFilters, TextMatches(*s.Text))
		if s.Label == nil {
			priorityFilters = append(priorityFilters, LabelMatches(*s.Text))
		}
	}
	if s.Value != nil {
		priorityFilters = append(priorityFilters, ValueMatches(*s.Value))
	}
	if s.Hint != nil {
		priorityFilters = append(priorityFilters, HintMatches(*s.Hint))
	}

	if len(priorityFilters) == 0 {
		constraintOnly := identityFilter
		if len(constraints) > 0 {
			constraintOnly = Intersect(constraints...)
		}
		if s.Nth != nil {
			return NthFilter(constraintOnly, *s.Nth)
		}
		return constraintOnly
	}

	broadened := func(cursors []*TreeCursor) []*TreeCursor {
		for _, content := range priorityFilters {
			combined := content
			if len(constraints) > 0 {
				combined = Intersect(append([]ElementFilter{content}, constraints...)...)
			}
			result := combined(cursors)
			if len(result) > 0 {
				if s.Nth != nil {
					return result
				}
				return rankCandidates(result)
			}
		}
		return []*TreeCursor{}
	}

	if s.Nth != nil {
		return NthFilter(broadened, *s.Nth)
	}
	return broadened
}

func identityFilter(cursors []*TreeCursor) []*TreeCursor {
	out := make([]*TreeCursor, len(cursors))
	copy(out, cursors)
	return out
}

// rankCandidates stable-sorts cursors so interactive and focused
// elements rank above inert duplicates: +1 for Clickable, +1 for
// Focused, highest score first. Cursors with an equal score keep
// their original document order.
func rankCandidates(cursors []*TreeCursor) []*TreeCursor {
	out := make([]*TreeCursor, len(cursors))
	copy(out, cursors)
	sort.SliceStable(out, func(i, j int) bool {
		return scoreCursor(out[i]) > scoreCursor(out[j])
	})
	return out
}

func scoreCursor(c *TreeCursor) int {
	score := 0
	if c.Node.Clickable != nil && *c.Node.Clickable {
		score++
	}
	if c.Node.Focused != nil && *c.Node.Focused {
		score++
	}
	return score
}
