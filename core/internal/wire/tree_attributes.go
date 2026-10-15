package wire

// AttrSecure is the canonical attribute key a driver sets on a node
// that holds a secret, such as a password input. AttrSecureTrue is its
// only value: a driver omits the attribute on every other node.
const (
	AttrSecure     = "secure"
	AttrSecureTrue = "true"
)

// canonicalAttributeKeys is the closed set of attribute keys a driver
// may emit without the "ext:" prefix. Any other key is platform
// extension data and must carry that prefix.
var canonicalAttributeKeys = map[string]bool{
	"id":       true,
	"text":     true,
	"label":    true,
	"hint":     true,
	"value":    true,
	"role":     true,
	"class":    true,
	"package":  true,
	"bounds":   true,
	AttrSecure: true,
}

// IsCanonicalAttribute reports whether key is in the canonical set.
func IsCanonicalAttribute(key string) bool { return canonicalAttributeKeys[key] }
