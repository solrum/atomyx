package orchestra

import (
	"fmt"

	"dev.atomyx/internal/wire"
)

// ObscurementAt decides, from the tree alone, whether another element
// renders on top of the midpoint of the node of root at path, using the
// same rules DetectObscurement applies before a tap. It makes no device
// call, so a viewer can show the verdict for a stored tree. It returns
// an error when path does not address a node of root.
func ObscurementAt(root wire.TreeNodeWire, path []int) (ObscurementResult, error) {
	target := cursorAt(FromTree(root), path)
	if target == nil {
		return ObscurementResult{}, nodePathError(path)
	}
	return DetectObscurement(root, target), nil
}

func nodePathError(path []int) error {
	return fmt.Errorf("node path %v does not address a node in the tree", path)
}
