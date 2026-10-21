package script

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// httpHeaderName is the token grammar of an HTTP header name.
var httpHeaderName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// validateHTTPRequestStep checks an httpRequest step's fields. Messages
// name the field and never repeat its value, because a value can carry
// a credential.
func validateHTTPRequestStep(s *HTTPRequestStep, path string, report reportFunc) {
	if !slices.Contains(HTTPMethods, s.Method) {
		report("%s.method: must be one of %s", path, strings.Join(HTTPMethods, ", "))
	}
	validateHTTPURL(s.URL, path+".url", report)

	for name, value := range s.Headers {
		if !httpHeaderName.MatchString(name) {
			report("%s.headers: %q is not a valid header name", path, name)
		}
		if strings.ContainsAny(value, "\r\n") {
			report("%s.headers.%s: a value must not contain a line break", path, name)
		}
	}
	if s.As != "" {
		validateCaptureVar(s.As, path+".as", report)
	}
	if s.ExpectStatus != nil && (*s.ExpectStatus < 100 || *s.ExpectStatus > 599) {
		report("%s.expectStatus: must be between 100 and 599", path)
	}
	if s.TimeoutMs != nil && (*s.TimeoutMs < 1 || *s.TimeoutMs > HTTPRequestMaxTimeoutMs) {
		report("%s.timeout: must be between 1 and %d milliseconds", path, HTTPRequestMaxTimeoutMs)
	}
	if s.Poll != nil {
		validateHTTPPoll(s, path, report)
	}
}

func validateHTTPURL(raw, path string, report reportFunc) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		report("%s: must be an absolute http or https URL with a host", path)
	}
}

func validateHTTPPoll(s *HTTPRequestStep, path string, report reportFunc) {
	if s.As == "" {
		report("%s.poll: requires `as`, because the condition reads the stored response", path)
	}
	if !strings.HasPrefix(s.Poll.Until, "$") {
		report("%s.poll.until: must be a JSON path starting with $", path)
	}
	if s.Poll.IntervalMs != nil && *s.Poll.IntervalMs < HTTPRequestMinPollIntervalMs {
		report("%s.poll.interval: must be at least %d milliseconds", path, HTTPRequestMinPollIntervalMs)
	}
}

// validateExtractRegexes rejects a pattern the regular expression engine
// cannot compile.
func validateExtractRegexes(s *ExtractStep, path string, report reportFunc) {
	for _, name := range s.Values.Keys() {
		value, _ := s.Values.Get(name)
		if value.Regex == nil {
			continue
		}
		if _, err := regexp.Compile(*value.Regex); err != nil {
			report("%s.values.%s.regex: not a valid regular expression", path, name)
		}
	}
}
