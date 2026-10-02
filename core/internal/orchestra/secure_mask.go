package orchestra

import "dev.atomyx/internal/wire"

// SecureMask replaces the text and value of a node that holds a
// secret. It has a fixed width so the output never reveals the length
// of the secret.
const SecureMask = "********"

// MaskSecureAttributes returns attrs with the text and value of a
// secure node replaced by SecureMask. A non-secure node, and a secure
// node whose text or value is empty, keep those attributes unchanged:
// an empty field carries nothing to hide. The label, hint, id, role
// and every other attribute are never touched. attrs itself is never
// modified, and it is returned as is when nothing needs masking.
func MaskSecureAttributes(attrs map[string]string) map[string]string {
	if attrs[AttrSecure] != wire.AttrSecureTrue {
		return attrs
	}
	if attrs[AttrText] == "" && attrs[AttrValue] == "" {
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

// MaskSecure returns a copy of root in which every secure node's text
// and value are replaced by SecureMask, as MaskSecureAttributes
// defines. It is for output only: selector resolution must keep using
// the real tree. root is never modified, and attribute maps that
// need no masking are shared with it, so the result must be treated as
// read-only.
func MaskSecure(root wire.TreeNodeWire) wire.TreeNodeWire {
	masked := root
	masked.Attributes = MaskSecureAttributes(root.Attributes)
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
