package wire

// AttrSecure is the canonical attribute key a driver sets on a node
// that holds a secret, such as a password input. AttrSecureTrue is its
// only value: a driver omits the attribute on every other node.
const (
	AttrSecure     = "secure"
	AttrSecureTrue = "true"
)
