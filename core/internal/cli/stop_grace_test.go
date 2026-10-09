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

func TestWorkerStopGraceOutlastsTheWorkerExitAndFitsInsideTheParentWatchdog(t *testing.T) {
	const killWait = 2 * time.Second
	if workerStopGrace <= shutdown.GracePeriod {
		t.Errorf("a worker gets %v to stop, no margin over the %v it spends on its own exit", workerStopGrace, shutdown.GracePeriod)
	}
	watchdog := shutdown.ParentGracePeriod - shutdown.ForcedCleanupBudget
	if workerStopGrace+killWait >= watchdog {
		t.Errorf("stopping a worker takes up to %v, which reaches the parent watchdog at %v", workerStopGrace+killWait, watchdog)
	}
}
