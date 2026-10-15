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
// Segment matching: for Label, Text, Value, and Hint, a literal that
// equals one whole newline-separated segment of an attribute value is
// tried immediately after the whole-value match of the same field, so
// a whole-value match always wins over a segment match. A literal ID
// that matched no node id is tried against label segments as the last
// entry, to reach an identifier folded into a parent's merged label.
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
	addContent := func(key string, p Pattern) {
		priorityFilters = append(priorityFilters, byAttr(key, p))
		if p.segmentCapable() {
			priorityFilters = append(priorityFilters, bySegment(key, p))
		}
	}
	if s.ID != nil {
		priorityFilters = append(priorityFilters, IDMatches(*s.ID))
	}
	if s.Label != nil {
		addContent(AttrLabel, *s.Label)
	}
	if s.Text != nil {
		addContent(AttrText, *s.Text)
		if s.Label == nil {
			addContent(AttrLabel, *s.Text)
		}
	}
	if s.Value != nil {
		addContent(AttrValue, *s.Value)
	}
	if s.Hint != nil {
		addContent(AttrHint, *s.Hint)
	}
	if s.ID != nil && s.ID.segmentCapable() {
		priorityFilters = append(priorityFilters, bySegment(AttrLabel, *s.ID))
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
