package orchestra

import "dev.atomyx/internal/wire"

// AttrSecure is the attribute key that marks a node holding a secret.
const AttrSecure = wire.AttrSecure

// IsSecure reports whether node holds a secret, such as a password
// input. It is the only reader of the secure attribute: callers never
// infer secrecy from the role, because the role vocabulary differs
// between platforms while the attribute does not.
func IsSecure(node wire.TreeNodeWire) bool {
	return node.Attributes[AttrSecure] == wire.AttrSecureTrue
}
