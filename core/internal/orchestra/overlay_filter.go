package orchestra

import (
	"strings"

	"dev.atomyx/internal/wire"
)

// systemOverlayResourcePrefixes lists resource-id prefixes belonging
// to OEM system overlays that ride along on every accessibility tree
// dump but are never a useful automation target.
//
//   - "com.samsung.android.app.cocktailbarservice:" is the Samsung Edge
//     Panel trigger handle (always present on One UI devices).
var systemOverlayResourcePrefixes = []string{
	"com.samsung.android.app.cocktailbarservice:",
}

// IsSystemOverlay reports whether node's resource id matches a known
// OEM system-overlay prefix. A caller walking a hierarchy typically
// wants to skip these nodes and everything beneath them; use
// FilterSystemOverlays to prune a whole tree in one call.
func IsSystemOverlay(node wire.TreeNodeWire) bool {
	id, ok := node.Attributes[AttrID]
	if !ok {
		return false
	}
	for _, prefix := range systemOverlayResourcePrefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// FilterSystemOverlays returns a copy of root with every node
// IsSystemOverlay accepts, and everything beneath it, removed. ok is
// false when root itself is a system overlay, in which case the
// returned tree is the zero value and there is nothing left to walk.
func FilterSystemOverlays(root wire.TreeNodeWire) (filtered wire.TreeNodeWire, ok bool) {
	if IsSystemOverlay(root) {
		return wire.TreeNodeWire{}, false
	}
	root.Children = filterOverlayChildren(root.Children)
	return root, true
}

func filterOverlayChildren(children []wire.TreeNodeWire) []wire.TreeNodeWire {
	if len(children) == 0 {
		return children
	}
	out := make([]wire.TreeNodeWire, 0, len(children))
	for _, child := range children {
		if IsSystemOverlay(child) {
			continue
		}
		child.Children = filterOverlayChildren(child.Children)
		out = append(out, child)
	}
	return out
}
