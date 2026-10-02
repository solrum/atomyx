package orchestra

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"dev.atomyx/internal/wire"
)

// MaxSuggestedValueLength is the longest attribute value, in runes,
// that SuggestSelectors turns into a literal pattern. A longer value
// is more likely to be prose than an identifier, and it makes a
// brittle selector.
const MaxSuggestedValueLength = 100

// SuggestedSelector is one candidate selector for a target node.
type SuggestedSelector struct {
	Selector Selector `json:"selector"`
	// Unique is true when resolving Selector against the same tree
	// picks the target node. A selector without Nth is unique only
	// when it also matches no other node. A selector with Nth is
	// unique because it pins the target by position, which is
	// fragile when the screen changes.
	Unique bool `json:"unique"`
	// MatchCount is how many nodes Selector matches, ignoring Nth.
	MatchCount int `json:"matchCount"`
}

// SuggestSelectors returns ranked candidate selectors for the node of
// root at path, the sequence of child indexes from the root, empty for
// the root itself. It returns an error when path does not address a
// node of root.
//
// Candidates are built from one content field each, in the priority
// broadening order: id, label, text, value, hint. A value is skipped
// when it is blank after trimming whitespace or longer than
// MaxSuggestedValueLength runes. The text and value of a secure node
// are never used.
//
// Uniqueness is decided by running CompileSelector against root and
// checking which node resolves first, never by a filter length alone.
// When a candidate is ambiguous, the smallest set of constraints
// among role, enabled, clickable and focused that makes it unique is
// added, using only values the target carries. When no candidate is
// unique, the best content field (or the role when there is none)
// is combined with Nth, which counts in document order. The result
// lists unique candidates first, in field order, then the rest.
// An empty result means the node carries no usable signal.
func SuggestSelectors(root wire.TreeNodeWire, path []int) ([]SuggestedSelector, error) {
	cursors := FromTree(root)
	target := cursorAt(cursors, path)
	if target == nil {
		return nil, fmt.Errorf("node path %v does not address a node in the tree", path)
	}
	s := &suggester{cursors: cursors, target: target}

	var unique, ambiguous []SuggestedSelector
	var firstBase *Selector
	for _, base := range s.contentCandidates() {
		base := base
		if firstBase == nil {
			firstBase = &base
		}
		c := s.evaluate(base)
		if c.Unique {
			unique = append(unique, c)
			continue
		}
		if c, ok := s.constrain(base); ok {
			unique = append(unique, c)
			continue
		}
		ambiguous = append(ambiguous, c)
	}
	if len(unique) == 0 {
		if c, ok := s.positional(firstBase); ok {
			unique = append(unique, c)
		}
	}
	return append(unique, ambiguous...), nil
}

type suggester struct {
	cursors []*TreeCursor
	target  *TreeCursor
}

func cursorAt(cursors []*TreeCursor, path []int) *TreeCursor {
	for _, c := range cursors {
		if pathEqual(c.Path(), path) {
			return c
		}
	}
	return nil
}

func pathEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *suggester) contentCandidates() []Selector {
	attrs := s.target.Node.Attributes
	secure := IsSecure(s.target.Node)
	var out []Selector
	add := func(key string, set func(*Selector, *Pattern)) {
		if secure && (key == AttrText || key == AttrValue) {
			return
		}
		v := attrs[key]
		if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > MaxSuggestedValueLength {
			return
		}
		p := LiteralPattern(v)
		var sel Selector
		set(&sel, &p)
		out = append(out, sel)
	}
	add(AttrID, func(sel *Selector, p *Pattern) { sel.ID = p })
	add(AttrLabel, func(sel *Selector, p *Pattern) { sel.Label = p })
	add(AttrText, func(sel *Selector, p *Pattern) { sel.Text = p })
	add(AttrValue, func(sel *Selector, p *Pattern) { sel.Value = p })
	add(AttrHint, func(sel *Selector, p *Pattern) { sel.Hint = p })
	return out
}

// evaluate resolves sel against the tree and reports the verdict.
func (s *suggester) evaluate(sel Selector) SuggestedSelector {
	counted := sel
	counted.Nth = nil
	matches := CompileSelector(counted)(s.cursors)
	picked := CompileSelector(sel)(s.cursors)
	resolvesToTarget := len(picked) > 0 && picked[0] == s.target
	unique := resolvesToTarget && (sel.Nth != nil || len(matches) == 1)
	return SuggestedSelector{Selector: sel, Unique: unique, MatchCount: len(matches)}
}

type constraint func(*Selector, *suggester) bool

var constraintOrder = [...]constraint{
	func(sel *Selector, s *suggester) bool {
		role, ok := s.target.Node.Attributes[AttrRole]
		if !ok || role == "" {
			return false
		}
		sel.Role = &role
		return true
	},
	func(sel *Selector, s *suggester) bool { return setTrue(&sel.Enabled, s.target.Node.Enabled) },
	func(sel *Selector, s *suggester) bool { return setTrue(&sel.Clickable, s.target.Node.Clickable) },
	func(sel *Selector, s *suggester) bool { return setTrue(&sel.Focused, s.target.Node.Focused) },
}

func setTrue(dst **bool, state *bool) bool {
	if state == nil || !*state {
		return false
	}
	t := true
	*dst = &t
	return true
}

// constrain adds the smallest, earliest subset of the target's role
// and true state flags that makes base unique.
func (s *suggester) constrain(base Selector) (SuggestedSelector, bool) {
	n := len(constraintOrder)
	for size := 1; size <= n; size++ {
		for mask := 1; mask < 1<<n; mask++ {
			if bitCount(mask) != size {
				continue
			}
			candidate := base
			applicable := true
			for i := 0; i < n && applicable; i++ {
				if mask&(1<<i) != 0 {
					applicable = constraintOrder[i](&candidate, s)
				}
			}
			if !applicable {
				continue
			}
			if c := s.evaluate(candidate); c.Unique {
				return c, true
			}
		}
	}
	return SuggestedSelector{}, false
}

func bitCount(mask int) int {
	n := 0
	for ; mask != 0; mask &= mask - 1 {
		n++
	}
	return n
}

// positional pins the target by document-order index within the
// match set of base, or of the role alone when base is nil.
func (s *suggester) positional(base *Selector) (SuggestedSelector, bool) {
	var sel Selector
	if base != nil {
		sel = *base
	}
	constraintOrder[0](&sel, s)
	if sel.IsEmpty() {
		return SuggestedSelector{}, false
	}
	order := make(map[*TreeCursor]int, len(s.cursors))
	for i, c := range s.cursors {
		order[c] = i
	}
	index := 0
	for _, c := range CompileSelector(sel)(s.cursors) {
		if order[c] < order[s.target] {
			index++
		}
	}
	sel.Nth = &index
	c := s.evaluate(sel)
	return c, c.Unique
}
