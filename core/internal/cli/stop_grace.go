package cli

import (
	"strings"
	"time"

	"dev.atomyx/internal/shutdown"
)

// StopGrace reports the exit budget of the command in args, the argv after
// the program name. A suite that shards over several devices stops worker
// processes on a stop signal, so it gets shutdown.ParentGracePeriod, and
// every other command, including a sharding worker, gets
// shutdown.GracePeriod. It only scans for the flags that mean sharding, so
// an argv the command later rejects costs nothing.
func StopGrace(args []string) time.Duration {
	if len(args) > 0 && args[0] == "driver" {
		args = args[1:]
	}
	if len(args) == 0 || args[0] != "suite" {
		return shutdown.GracePeriod
	}
	var sharded, worker bool
	for _, arg := range args[1:] {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--devices", "--shard":
			sharded = true
		case "--worker":
			worker = true
		}
	}
	if sharded && !worker {
		return shutdown.ParentGracePeriod
	}
	return shutdown.GracePeriod
}
