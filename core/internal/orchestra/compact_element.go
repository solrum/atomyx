package orchestra

import (
	"context"
	"fmt"

	"dev.atomyx/internal/wire"
)

// CompactElement is a flat, single-level view of one tree node, used
// by the transition-diagnostics functions (DetectLoading,
// DetectMotion, DiffAppeared, AnalyzeOverlay, ClassifyFailure) so
// they can reason over a plain slice instead of walking wire.TreeNodeWire
// recursively themselves.
type CompactElement struct {
	ElementID  string
	Role       string
	Label      string
	Text       string
	ResourceID string
	Bounds     wire.Bounds
	Clickable  bool
	Enabled    bool
	// Selector is nil when the node carries none of resourceId,
	// contentDesc, or text. Callers read it with a plain map index
	// (el.Selector["resourceId"]), which returns the empty string for
	// a nil map exactly as it would for an empty one, so no caller
	// needs to nil-check before reading a specific key.
	Selector map[string]string
}

// TreeNodeToCompactElements flattens root into a CompactElement list
// in pre-order (matching Walk's traversal, since both implement the
// same parent-before-children, earlier-sibling-before-later-sibling
// stack walk). Nodes with no parseable bounds attribute are dropped:
// they have no geometry for the diagnostic functions to reason about
// and typically represent invisible semantic grouping nodes.
func TreeNodeToCompactElements(root wire.TreeNodeWire) []CompactElement {
	cursors := Walk(root)
	out := make([]CompactElement, 0, len(cursors))
	counter := 0
	for _, c := range cursors {
		bounds, ok := ParseBounds(c.Node.Attributes[AttrBounds])
		if !ok {
			continue
		}
		resourceID := c.Node.Attributes[AttrID]
		label := c.Node.Attributes[AttrLabel]
		text := c.Node.Attributes[AttrText]
		role := c.Node.Attributes[AttrRole]

		var selector map[string]string
		if resourceID != "" || label != "" || text != "" {
			selector = make(map[string]string, 3)
			if resourceID != "" {
				selector["resourceId"] = resourceID
			}
			if label != "" {
				selector["contentDesc"] = label
			}
			if text != "" {
				selector["text"] = text
			}
		}

		out = append(out, CompactElement{
			ElementID:  fmt.Sprintf("tree-%d", counter),
			Role:       role,
			Label:      label,
			Text:       text,
			ResourceID: resourceID,
			Bounds:     bounds,
			Clickable:  c.Node.Clickable != nil && *c.Node.Clickable,
			Enabled:    c.Node.Enabled == nil || *c.Node.Enabled,
			Selector:   selector,
		})
		counter++
	}
	return out
}

// ResolvedElementState is the shape of an already-resolved element
// (from Orchestra.FindOne), used by DetectTargetStateChange to
// compare the tap target's state before and after a tap. Enabled and
// Clickable stay nil when the underlying accessibility runtime did
// not report the value, which DetectTargetStateChange treats as
// distinct from an explicit false.
type ResolvedElementState struct {
	Found     bool
	Role      string
	Enabled   *bool
	Clickable *bool
	Bounds    *wire.Bounds
}

// ResolveElementState resolves selector's first match and reports its
// role, enabled, clickable, and bounds state. Found is false when no
// element matches selector; every other field is then zero.
func (o *Orchestra) ResolveElementState(ctx context.Context, selector Selector) (ResolvedElementState, error) {
	cursor, err := o.FindOne(ctx, selector)
	if err != nil {
		return ResolvedElementState{}, err
	}
	if cursor == nil {
		return ResolvedElementState{Found: false}, nil
	}
	attrs := cursor.Node.Attributes
	state := ResolvedElementState{
		Found:     true,
		Role:      attrs[AttrRole],
		Enabled:   cursor.Node.Enabled,
		Clickable: cursor.Node.Clickable,
	}
	if bounds, ok := ParseBounds(attrs[AttrBounds]); ok {
		state.Bounds = &bounds
	}
	return state, nil
}
