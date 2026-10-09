package cli

import (
	"testing"
	"time"

	"dev.atomyx/internal/shutdown"
)

func TestStopGraceIsLongerOnlyForASuiteThatShards(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want time.Duration
	}{
		{"shard auto", []string{"suite", "--glob", "*.yml", "--shard", "auto"}, shutdown.ParentGracePeriod},
		{"devices", []string{"suite", "smoke", "--devices", "a,b"}, shutdown.ParentGracePeriod},
		{"driver module form", []string{"driver", "suite", "--devices", "a,b"}, shutdown.ParentGracePeriod},
		{"single device", []string{"suite", "--device", "a"}, shutdown.GracePeriod},
		{"worker", []string{"suite", "--device", "a", "--worker", "--devices", "a,b"}, shutdown.GracePeriod},
		{"run", []string{"run", "--file", "a.yml"}, shutdown.GracePeriod},
		{"inline value", []string{"suite", "--shard=auto"}, shutdown.ParentGracePeriod},
		{"empty", nil, shutdown.GracePeriod},
	}
	for _, tc := range cases {
		if got := StopGrace(tc.args); got != tc.want {
			t.Errorf("%s: StopGrace(%v) = %v, want %v", tc.name, tc.args, got, tc.want)
		}
	}
}
