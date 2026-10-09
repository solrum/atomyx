package device

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"dev.atomyx/internal/process"
)

const (
	// orphanParentPid is the parent process id of a process whose parent
	// exited, because the system reparents it to the init process.
	orphanParentPid = 1
	// orphanPollInterval is how often the reaper checks whether an ended
	// recorder is gone.
	orphanPollInterval = 100 * time.Millisecond
)

// simulatorOrphanReaper ends simulator recorders that outlived the atomyx
// process that started them. A recorder runs in its own process group, so
// when atomyx dies without cleaning up, for example after SIGKILL, the
// recorder keeps running, and the simulator then refuses every later
// recording with "Host recording is already in progress".
type simulatorOrphanReaper struct {
	runner process.Runner
	// grace bounds how long an interrupted recorder may take to exit
	// before it is killed.
	grace time.Duration
}

// leftoverRecorder is one running simctl recordVideo process.
type leftoverRecorder struct {
	pid  int
	ppid int
}

// reap ends every recorder of udid whose parent is gone. A recorder that
// still has a live parent belongs to another process, so reap leaves it
// alone and fails, because starting a second recording would fail anyway.
// The device lease guarantees atomyx itself holds no other recording of
// udid while reap runs.
func (r simulatorOrphanReaper) reap(ctx context.Context, udid string) error {
	leftovers, err := r.list(ctx, udid)
	if err != nil {
		return err
	}
	for _, rec := range leftovers {
		if rec.ppid != orphanParentPid {
			return fmt.Errorf("another process (pid %d) is already recording simulator %s", rec.pid, udid)
		}
	}
	for _, rec := range leftovers {
		if err := r.end(ctx, udid, rec.pid); err != nil {
			return err
		}
	}
	return nil
}

// end interrupts the recorder so it exits, escalating to a kill when it
// does not exit within the grace period.
func (r simulatorOrphanReaper) end(ctx context.Context, udid string, pid int) error {
	for _, signal := range []string{"-INT", "-KILL"} {
		if _, err := r.runner.Run(ctx, "kill", signal, strconv.Itoa(pid)); err != nil {
			return fmt.Errorf("end the leftover recorder (pid %d): %w", pid, err)
		}
		gone, err := r.waitGone(ctx, udid, pid)
		if err != nil {
			return err
		}
		if gone {
			return nil
		}
	}
	return fmt.Errorf("the leftover recorder (pid %d) of simulator %s did not exit", pid, udid)
}

func (r simulatorOrphanReaper) waitGone(ctx context.Context, udid string, pid int) (bool, error) {
	deadline := time.Now().Add(r.grace)
	for {
		leftovers, err := r.list(ctx, udid)
		if err != nil {
			return false, err
		}
		gone := true
		for _, rec := range leftovers {
			gone = gone && rec.pid != pid
		}
		if gone {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(orphanPollInterval):
		}
	}
}

func (r simulatorOrphanReaper) list(ctx context.Context, udid string) ([]leftoverRecorder, error) {
	res, err := r.runner.Run(ctx, "ps", "-axo", "pid=,ppid=,command=")
	if err != nil {
		return nil, fmt.Errorf("list host processes: %w", err)
	}
	var found []leftoverRecorder
	for _, line := range strings.Split(res.Stdout, "\n") {
		if rec, ok := parseLeftoverRecorder(line, udid); ok {
			found = append(found, rec)
		}
	}
	return found, nil
}

// parseLeftoverRecorder reads one line of ps output and reports whether it
// is a simctl recordVideo process of udid.
func parseLeftoverRecorder(line, udid string) (leftoverRecorder, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return leftoverRecorder{}, false
	}
	pid, pidErr := strconv.Atoi(fields[0])
	ppid, ppidErr := strconv.Atoi(fields[1])
	if pidErr != nil || ppidErr != nil {
		return leftoverRecorder{}, false
	}
	command := fields[2:]
	for i := 0; i+3 < len(command); i++ {
		if path.Base(command[i]) == "simctl" && command[i+1] == "io" && command[i+2] == udid && command[i+3] == "recordVideo" {
			return leftoverRecorder{pid: pid, ppid: ppid}, true
		}
	}
	return leftoverRecorder{}, false
}
