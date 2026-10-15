package orchestra

// ElementFilter is a pure function from a list of cursors to a
// (possibly smaller) list of cursors. Filters compose via Intersect
// to express AND logic; CompileSelector builds the priority-
// broadening policy on top of these primitives.
type ElementFilter func(cursors []*TreeCursor) []*TreeCursor

func byAttr(key string, p Pattern) ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0, len(cursors))
		for _, c := range cursors {
			if v, ok := c.Node.Attributes[key]; ok && p.Matches(v) {
				out = append(out, c)
			}
		}
		return out
	}
}

// bySegment filters cursors whose attribute holds newline-separated
// segments, one of which equals p exactly.
func bySegment(key string, p Pattern) ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0)
		for _, c := range cursors {
			if v, ok := c.Node.Attributes[key]; ok && p.MatchesSegment(v) {
				out = append(out, c)
			}
		}
		return out
	}
}

// IDMatches filters cursors whose "id" attribute matches p.
func IDMatches(p Pattern) ElementFilter { return byAttr(AttrID, p) }

// TextMatches filters cursors whose "text" attribute matches p.
func TextMatches(p Pattern) ElementFilter { return byAttr(AttrText, p) }

// LabelMatches filters cursors whose "label" attribute matches p.
func LabelMatches(p Pattern) ElementFilter { return byAttr(AttrLabel, p) }

// HintMatches filters cursors whose "hint" attribute matches p.
func HintMatches(p Pattern) ElementFilter { return byAttr(AttrHint, p) }

// ValueMatches filters cursors whose "value" attribute matches p.
func ValueMatches(p Pattern) ElementFilter { return byAttr(AttrValue, p) }

// RoleIs filters cursors whose "role" attribute equals role exactly.
func RoleIs(role string) ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0, len(cursors))
		for _, c := range cursors {
			if v, ok := c.Node.Attributes[AttrRole]; ok && v == role {
				out = append(out, c)
			}
		}
		return out
	}
}

// IsEnabled filters cursors whose node explicitly reports Enabled
// true. A nil or false value is excluded.
func IsEnabled() ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0, len(cursors))
		for _, c := range cursors {
			if c.Node.Enabled != nil && *c.Node.Enabled {
				out = append(out, c)
			}
		}
		return out
	}
}

// IsClickable filters cursors whose node explicitly reports
// Clickable true. A nil or false value is excluded.
func IsClickable() ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0, len(cursors))
		for _, c := range cursors {
			if c.Node.Clickable != nil && *c.Node.Clickable {
				out = append(out, c)
			}
		}
		return out
	}
}

// IsFocused filters cursors whose node explicitly reports Focused
// true. A nil or false value is excluded.
func IsFocused() ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		out := make([]*TreeCursor, 0, len(cursors))
		for _, c := range cursors {
			if c.Node.Focused != nil && *c.Node.Focused {
				out = append(out, c)
			}
		}
		return out
	}
}

// Intersect combines filters with AND semantics, short-circuiting to
// an empty result as soon as an intermediate result is empty.
func Intersect(filters ...ElementFilter) ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		result := cursors
		for _, f := range filters {
			result = f(result)
			if len(result) == 0 {
				return []*TreeCursor{}
			}
		}
		out := make([]*TreeCursor, len(result))
		copy(out, result)
		return out
	}
}

// NthFilter selects the index-th cursor (zero-based) that filter
// produces, in the order filter returns them. It returns an empty
// result when index is out of range.
func NthFilter(filter ElementFilter, index int) ElementFilter {
	return func(cursors []*TreeCursor) []*TreeCursor {
		r := filter(cursors)
		if index < 0 || index >= len(r) {
			return []*TreeCursor{}
		}
		return []*TreeCursor{r[index]}
	}
}
