package history

import "time"

// ParseSince reads a lower time bound as epoch milliseconds. It accepts
// an RFC 3339 timestamp or a YYYY-MM-DD date taken as midnight UTC, and
// reports false for anything else.
func ParseSince(raw string) (int64, bool) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UnixMilli(), true
	}
	if t, err := time.Parse(time.DateOnly, raw); err == nil {
		return t.UnixMilli(), true
	}
	return 0, false
}
