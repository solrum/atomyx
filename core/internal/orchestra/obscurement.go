package orchestra

import "dev.atomyx/internal/wire"

// Semantic roles this package branches on. The normalized vocabulary
// carries many more roles on the wire; only the values obscurement
// and the wait primitives actually switch on are named here.
const (
	RoleContainer       = "container"
	RoleOther           = "other"
	RoleKeyboard        = "keyboard"
	RoleTextField       = "text-field"
	RoleSecureTextField = "secure-text-field"
	RoleSearchField     = "search-field"
)

// ObscurementResult is the outcome of DetectObscurement. Obscured
// false means the target is safe to tap and Obscurer is nil.
// Obscured true means some other element renders on top of the
// target's midpoint, identified by Obscurer.
type ObscurementResult struct {
	Obscured bool          `json:"obscured"`
	Obscurer *ObscurerInfo `json:"obscurer,omitempty"`
}

// DetectObscurement decides whether some element other than target
// is rendered on top of target's midpoint, within the tree rooted at
// root. target must be reachable from root; the caller typically
// obtains root via RootOf(target) so both are the exact tree the
// caller already resolved target from. See Path's doc comment for
// why that matters here.
//
// Algorithm:
//
//  1. Walk root in pre-order. For every node whose bounds contain
//     target's midpoint, remember it as topmost. Pre-order visits
//     parents before children and earlier siblings before later
//     ones, so the LAST node seen that contains the point is the
//     topmost in z-order: later siblings (and their children) render
//     on top of earlier ones in every renderer Atomyx supports.
//     Descendants of target are skipped: they are drawn inside the
//     target and belong to it, for example the clickable button a
//     semantics wrapper exposes over the same frame.
//  2. If topmost is target itself, or an ancestor of target, it is
//     not an obscurer: an ancestor's frame always encloses its
//     descendants by construction, so flagging that as obscurement
//     would falsely reject every element nested inside a container.
//  3. If topmost carries role "container" or "other" and no id or
//     label, treat it as a transparent layout wrapper rather than a
//     rendering blocker. A real modal, sheet, alert, or floating
//     button always carries a distinctive role or a non-empty
//     identifier or label.
//  4. Otherwise topmost is the obscurer.
//
// If target carries no parseable bounds attribute, DetectObscurement
// returns Obscured false: it cannot reason about z-order without
// geometry, and fails open rather than blocking a legitimate tap.
func DetectObscurement(root wire.TreeNodeWire, target *TreeCursor) ObscurementResult {
	targetBounds, ok := ParseBounds(target.Node.Attributes[AttrBounds])
	if !ok {
		return ObscurementResult{}
	}
	center := BoundsCenter(targetBounds)

	targetPath := target.Path()
	var topmost *TreeCursor
	for _, c := range Walk(root) {
		if p := c.Path(); len(p) > len(targetPath) && isAncestorOrSelfPath(targetPath, p) {
			continue
		}
		if b, ok := ParseBounds(c.Node.Attributes[AttrBounds]); ok && BoundsContain(b, center.X, center.Y) {
			topmost = c
		}
	}
	if topmost == nil {
		return ObscurementResult{}
	}
	if isAncestorOrSelfPath(topmost.Path(), target.Path()) {
		return ObscurementResult{}
	}

	role := topmost.Node.Attributes[AttrRole]
	id := topmost.Node.Attributes[AttrID]
	label := topmost.Node.Attributes[AttrLabel]
	if (role == RoleContainer || role == RoleOther) && id == "" && label == "" {
		return ObscurementResult{}
	}

	return ObscurementResult{Obscured: true, Obscurer: &ObscurerInfo{Role: role, ID: id, Label: label}}
}

// isAncestorOrSelfPath reports whether ancestor is a prefix of
// descendant, meaning the node at ancestor's tree position is that
// node itself or one of its ancestors.
func isAncestorOrSelfPath(ancestor, descendant []int) bool {
	if len(ancestor) > len(descendant) {
		return false
	}
	for i, v := range ancestor {
		if descendant[i] != v {
			return false
		}
	}
	return true
}
