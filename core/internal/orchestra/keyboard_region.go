package orchestra

import "dev.atomyx/internal/wire"

// keyboardRegion returns the screen area the on-screen keyboard covers
// in tree, or nil when no keyboard is shown or its frame cannot be
// trusted. A frame is untrusted when it is empty or does not fit inside
// the screen, because a frame reported in a different coordinate space
// than the rest of the hierarchy would shrink the viewport by an
// arbitrary amount and make reachable targets look covered.
func keyboardRegion(tree wire.TreeNodeWire, screen wire.Size) *wire.Bounds {
	state := readKeyboardState(tree)
	if !state.Visible || state.Bounds == nil {
		return nil
	}
	b := *state.Bounds
	if b.Right <= b.Left || b.Bottom <= b.Top {
		return nil
	}
	if b.Left < 0 || b.Top < 0 || b.Right > screen.Width || b.Bottom > screen.Height {
		return nil
	}
	return &b
}

// coveredByKeyboard reports whether the midpoint of bounds sits under
// the keyboard region of tree.
func coveredByKeyboard(tree wire.TreeNodeWire, bounds wire.Bounds, screen wire.Size) bool {
	region := keyboardRegion(tree, screen)
	if region == nil {
		return false
	}
	center := BoundsCenter(bounds)
	return BoundsContain(*region, center.X, center.Y)
}

// visibleScreen returns screen with its height cut at the top edge of
// the keyboard region of tree, or screen unchanged when no trusted
// keyboard is shown.
func visibleScreen(tree wire.TreeNodeWire, screen wire.Size) wire.Size {
	if region := keyboardRegion(tree, screen); region != nil {
		screen.Height = region.Top
	}
	return screen
}
