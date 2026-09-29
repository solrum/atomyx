package orchestra

import "dev.atomyx/internal/wire"

// Find runs filter against every cursor in root and returns the
// complete match list, in the order filter produces them.
func Find(filter ElementFilter, root wire.TreeNodeWire) []*TreeCursor {
	return filter(FromTree(root))
}

// FindOne runs filter against root and returns its first match. The
// second return value is false when filter produced no match.
func FindOne(filter ElementFilter, root wire.TreeNodeWire) (*TreeCursor, bool) {
	matches := Find(filter, root)
	if len(matches) == 0 {
		return nil, false
	}
	return matches[0], true
}

// ResolvedPoint is the coordinate-resolution outcome for a single
// matched cursor: the tap-target center Orchestra's action pipeline
// computes once scroll-into-view and obscurement detection (both
// stateful, device-driving concerns outside this package) have
// cleared. Bounds and Point are nil when the matched node carries no
// parseable "bounds" attribute.
type ResolvedPoint struct {
	Cursor *TreeCursor
	Bounds *wire.Bounds
	Point  *wire.Point
}

// ResolveFirst runs filter against root, and for its first match
// parses the "bounds" attribute and computes its center point. The
// second return value is false when filter produced no match.
func ResolveFirst(filter ElementFilter, root wire.TreeNodeWire) (ResolvedPoint, bool) {
	cursor, ok := FindOne(filter, root)
	if !ok {
		return ResolvedPoint{}, false
	}
	resolved := ResolvedPoint{Cursor: cursor}
	if raw, present := cursor.Node.Attributes[AttrBounds]; present {
		if bounds, parsed := ParseBounds(raw); parsed {
			center := BoundsCenter(bounds)
			resolved.Bounds = &bounds
			resolved.Point = &center
		}
	}
	return resolved, true
}
