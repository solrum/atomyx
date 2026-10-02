package orchestra

import "dev.atomyx/internal/wire"

// SecureMask replaces the text and value of a node that holds a
// secret. Every secret is replaced by the same string, so the output
// reveals neither the content nor the length of the secret.
const SecureMask = "********"

// MaskSecure returns a copy of root in which the text and value of
// every secure node (see IsSecure) are replaced by SecureMask. An
// empty text or value stays empty, because it carries nothing to
// hide. The label, hint, id, role and every other attribute are never
// touched. It is for output only: selector resolution must keep using
// the real tree. root is never modified, and attribute maps that need
// no masking are shared with it, so the result must be treated as
// read-only.
func MaskSecure(root wire.TreeNodeWire) wire.TreeNodeWire {
	masked := root
	masked.Attributes = maskSecureAttributes(root)
	if len(root.Children) == 0 {
		return masked
	}
	children := make([]wire.TreeNodeWire, len(root.Children))
	for i, child := range root.Children {
		children[i] = MaskSecure(child)
	}
	masked.Children = children
	return masked
}

func maskSecureAttributes(node wire.TreeNodeWire) map[string]string {
	attrs := node.Attributes
	if !IsSecure(node) || (attrs[AttrText] == "" && attrs[AttrValue] == "") {
		return attrs
	}
	masked := make(map[string]string, len(attrs))
	for key, value := range attrs {
		masked[key] = value
	}
	for _, key := range [...]string{AttrText, AttrValue} {
		if masked[key] != "" {
			masked[key] = SecureMask
		}
	}
	return masked
}
