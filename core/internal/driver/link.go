package driver

import (
	"fmt"
	"regexp"
	"unicode"

	"dev.atomyx/internal/diag"
)

// MaxLinkLength is the longest link, in bytes, that OpenLink accepts.
const MaxLinkLength = 8192

// linkSchemePattern matches the scheme that starts an absolute link.
var linkSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// ValidateLink rejects a link that cannot be handed to a device. A link
// must be non-empty, at most MaxLinkLength bytes, free of control
// characters, and start with a scheme such as "https:" or "myapp:". The
// error carries diag.CodeInvalidArgument.
//
// Spaces and shell metacharacters are accepted, because adapters deliver
// the link without interpreting it.
func ValidateLink(link string) error {
	switch {
	case link == "":
		return invalidLink("the link must not be empty")
	case len(link) > MaxLinkLength:
		return invalidLink(fmt.Sprintf("the link is longer than %d bytes", MaxLinkLength))
	case !linkSchemePattern.MatchString(link):
		return invalidLink("the link must start with a scheme, for example https://example.com or myapp://home")
	}
	for _, r := range link {
		if unicode.IsControl(r) {
			return invalidLink("the link must not contain control characters")
		}
	}
	return nil
}

func invalidLink(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
