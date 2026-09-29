package orchestra

// Canonical attribute keys read from wire.TreeNodeWire.Attributes.
// These mirror the cross-platform attribute vocabulary every driver
// normalizes into: Android's resourceId / contentDescription /
// hintText and iOS's accessibilityIdentifier / accessibilityLabel /
// placeholderValue all land on the same key here, so selector
// matching never branches on which platform produced the tree.
const (
	AttrID      = "id"
	AttrText    = "text"
	AttrLabel   = "label"
	AttrHint    = "hint"
	AttrValue   = "value"
	AttrRole    = "role"
	AttrClass   = "class"
	AttrPackage = "package"
	AttrBounds  = "bounds"
)
