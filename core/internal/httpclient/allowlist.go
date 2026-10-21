package httpclient

import (
	"net/url"
	"strings"
)

// hostPattern is one http.allowHosts entry: a host name or address, an
// optional port, and an optional leading "*." that matches any
// subdomain. An entry without a port matches every port.
type hostPattern struct {
	host     string
	port     string
	wildcard bool
}

func parseHostPattern(entry string) hostPattern {
	pattern := hostPattern{}
	rest := strings.ToLower(strings.TrimSpace(entry))
	if strings.HasPrefix(rest, "[") {
		if end := strings.Index(rest, "]"); end > 0 {
			pattern.host = rest[1:end]
			if tail := rest[end+1:]; strings.HasPrefix(tail, ":") {
				pattern.port = tail[1:]
			}
			return pattern
		}
	}
	if strings.Count(rest, ":") == 1 {
		i := strings.Index(rest, ":")
		pattern.port = rest[i+1:]
		rest = rest[:i]
	}
	if strings.HasPrefix(rest, "*.") {
		pattern.wildcard = true
		rest = rest[1:]
	}
	pattern.host = rest
	return pattern
}

func (p hostPattern) matches(host, port string) bool {
	if p.port != "" && p.port != port {
		return false
	}
	if p.wildcard {
		return strings.HasSuffix(host, p.host) && len(host) > len(p.host)
	}
	return host == p.host
}

// allowlist is the parsed http.allowHosts list.
type allowlist []hostPattern

func newAllowlist(entries []string) allowlist {
	list := make(allowlist, 0, len(entries))
	for _, entry := range entries {
		list = append(list, parseHostPattern(entry))
	}
	return list
}

// allows reports whether the host and port of target are listed. A
// target without an explicit port uses the default port of its scheme.
func (a allowlist) allows(target *url.URL) bool {
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	if port == "" {
		port = defaultPort(target.Scheme)
	}
	for _, pattern := range a {
		if pattern.matches(host, port) {
			return true
		}
	}
	return false
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
