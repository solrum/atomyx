package netcapture

import "strings"

// ParseConfigString parses a capture target string of the form
// "type:path" into a Config, the same shape every host that lets an
// operator or an agent name a capture target (the CLI's --proxy flag,
// the MCP server's run_script "proxy" argument) accepts. A value with
// no ":", or one that starts with it, is treated as a bare path,
// defaulting to the "file" adapter. Otherwise the text before the
// first ":" is the adapter type and the rest is its path. Returns nil
// for an empty string, which New maps to no capture.
func ParseConfigString(s string) *Config {
	if s == "" {
		return nil
	}
	colonIdx := strings.IndexByte(s, ':')
	if colonIdx <= 0 {
		return &Config{Type: "file", Path: s}
	}
	return &Config{Type: s[:colonIdx], Path: s[colonIdx+1:]}
}
