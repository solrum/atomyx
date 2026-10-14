package config

import "testing"

func TestRetentionBounds(t *testing.T) {
	keep, days := int64(20), int64(14)
	tests := []struct {
		name     string
		r        *Retention
		wantKeep int
		wantDays int
	}{
		{"nil", nil, 0, 0},
		{"keep only", &Retention{KeepLast: &keep}, 20, 0},
		{"days only", &Retention{OlderThanDays: &days}, 0, 14},
		{"both", &Retention{KeepLast: &keep, OlderThanDays: &days}, 20, 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKeep, gotDays := tt.r.Bounds()
			if gotKeep != tt.wantKeep || gotDays != tt.wantDays {
				t.Fatalf("Bounds() = (%d, %d), want (%d, %d)", gotKeep, gotDays, tt.wantKeep, tt.wantDays)
			}
		})
	}
}
