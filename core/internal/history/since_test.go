package history

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	midnight := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC).UnixMilli()
	tests := []struct {
		raw    string
		want   int64
		wantOK bool
	}{
		{"2026-10-14", midnight, true},
		{"2026-10-14T00:00:00Z", midnight, true},
		{"2026-10-14T02:00:00+02:00", midnight, true},
		{"", 0, false},
		{"yesterday", 0, false},
		{"14/10/2026", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := ParseSince(tc.raw)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("ParseSince(%q) = %d, %v, want %d, %v", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
