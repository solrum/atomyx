package orchestra

import "dev.atomyx/internal/wire"

// TreeCursor is a navigation wrapper around a wire.TreeNodeWire that
// preserves the parent pointer during a tree walk, so structural
// filters can climb back up without the wire shape itself carrying a
// circular parent reference.
type TreeCursor struct {
	Node wire.TreeNodeWire
	// Parent is nil for the root cursor.
	Parent *TreeCursor
	// Depth is the cursor's distance from the root; the root is 0.
	Depth int
	// ChildIndex is the cursor's zero-based index among its
	// parent's children. The root is 0.
	ChildIndex int
}

// Walk visits root in pre-order (parent before children, children in
// original order) and returns one cursor per node.
func Walk(root wire.TreeNodeWire) []*TreeCursor {
	rootCursor := &TreeCursor{Node: root, Parent: nil, Depth: 0, ChildIndex: 0}
	result := make([]*TreeCursor, 0)
	stack := []*TreeCursor{rootCursor}
	for len(stack) > 0 {
		cursor := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		result = append(result, cursor)
		children := cursor.Node.Children
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, &TreeCursor{
				Node:       children[i],
				Parent:     cursor,
				Depth:      cursor.Depth + 1,
				ChildIndex: i,
			})
		}
	}
	return result
}

// FromTree produces a filterable cursor list from a fresh tree.
// Equivalent to Walk(root), named for callers that read more
// intent-fully as "the list of cursors" than "a walk".
func FromTree(root wire.TreeNodeWire) []*TreeCursor {
	return Walk(root)
}

// RootOf walks cursor's parent chain up to the root and returns the
// root's node, without re-walking the tree. Orchestra's obscurement
// pass uses this to recover the tree a resolved cursor points into,
// rather than issuing a fresh hierarchy call that would produce an
// unrelated wire.TreeNodeWire value.
func RootOf(cursor *TreeCursor) wire.TreeNodeWire {
	c := cursor
	for c.Parent != nil {
		c = c.Parent
	}
	return c.Node
}

// Path returns the sequence of child indices from the root to this
// cursor, empty for the root cursor. wire.TreeNodeWire is a plain
// value type with no object identity, so two cursors produced by
// separate Walk calls over an identical tree value occupy the same
// tree position exactly when their Path results are equal. Orchestra
// uses this equality to recover the reference-identity checks the
// obscurement algorithm depends on.
func (c *TreeCursor) Path() []int {
	if c.Parent == nil {
		return []int{}
	}
	return append(c.Parent.Path(), c.ChildIndex)
}
