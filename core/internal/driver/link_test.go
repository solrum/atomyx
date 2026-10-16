package driver

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
)

func TestValidateLink(t *testing.T) {
	accepted := []string{
		"https://example.com",
		"https://example.com/a?x=1&y=2#frag",
		"myapp://open/focus",
		"myapp:home",
		"a+b.c-d://x",
		"myapp://say/it's \"quoted\" with spaces",
		"tel:+15551234567",
	}
	for _, link := range accepted {
		if err := ValidateLink(link); err != nil {
			t.Errorf("ValidateLink(%q) = %v, want nil", link, err)
		}
	}

	rejected := map[string]string{
		"":                 "must not be empty",
		"example.com/path": "must start with a scheme",
		"://missing":       "must start with a scheme",
		"1http://x":        "must start with a scheme",
		"myapp://a\nb":     "control characters",
		"myapp://a\x00b":   "control characters",
		"myapp://" + strings.Repeat("a", MaxLinkLength): "longer than",
	}
	for link, want := range rejected {
		err := ValidateLink(link)
		if err == nil {
			t.Errorf("ValidateLink(%.20q) = nil, want an error containing %q", link, want)
			continue
		}
		if code, message, _ := diag.Describe(err); code != diag.CodeInvalidArgument || !strings.Contains(message, want) {
			t.Errorf("ValidateLink(%.20q) = code %q message %q, want %q containing %q", link, code, message, diag.CodeInvalidArgument, want)
		}
	}
}
