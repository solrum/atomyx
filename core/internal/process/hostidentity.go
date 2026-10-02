package process

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// psStartTimeLayout is the layout "ps -o lstart=" prints in the C
// locale, for example "Thu Oct  2 10:15:02 2026". The value carries no
// zone and is in the host's local time.
const psStartTimeLayout = "Mon Jan _2 15:04:05 2006"

// procStatBootTimeKey names the line of /proc/stat that holds the boot
// time as seconds since the epoch.
const procStatBootTimeKey = "btime"

// StartTime reports when the process pid started, to the second. Paired
// with a pid it identifies one process for the lifetime of the host,
// because the operating system can hand a pid to an unrelated process
// after the first one exits.
func StartTime(ctx context.Context, runner Runner, pid int) (time.Time, error) {
	res, err := runner.Run(WithEnv(ctx, "LC_ALL=C"), "ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	if err != nil {
		return time.Time{}, fmt.Errorf("read the start time of process %d: %w", pid, err)
	}
	text := strings.Join(strings.Fields(res.Stdout), " ")
	started, err := time.ParseInLocation(strings.Join(strings.Fields(psStartTimeLayout), " "), text, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse the start time %q of process %d: %w", text, pid, err)
	}
	return started.UTC(), nil
}

// BootTime reports when the host booted, to the second. A pid recorded
// before a reboot can name an unrelated process afterwards, so a
// different boot time invalidates every recorded pid.
func BootTime(ctx context.Context, runner Runner) (time.Time, error) {
	if runtime.GOOS == "linux" {
		return linuxBootTime()
	}
	res, err := runner.Run(ctx, "sysctl", "-n", "kern.boottime")
	if err != nil {
		return time.Time{}, fmt.Errorf("read the host boot time: %w", err)
	}
	return parseSysctlBootTime(res.Stdout)
}

// parseSysctlBootTime extracts the seconds field of the value
// "sysctl -n kern.boottime" prints, for example
// "{ sec = 1759372800, usec = 41230 } Thu Oct  2 07:20:00 2026".
func parseSysctlBootTime(out string) (time.Time, error) {
	_, rest, ok := strings.Cut(out, "{ sec =")
	if !ok {
		return time.Time{}, fmt.Errorf("parse the host boot time %q: no seconds field", strings.TrimSpace(out))
	}
	digits, _, _ := strings.Cut(rest, ",")
	sec, err := strconv.ParseInt(strings.TrimSpace(digits), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse the host boot time %q: %w", strings.TrimSpace(out), err)
	}
	return time.Unix(sec, 0).UTC(), nil
}

func linuxBootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read the host boot time: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == procStatBootTimeKey {
			sec, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse the host boot time %q: %w", fields[1], err)
			}
			return time.Unix(sec, 0).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("read the host boot time: no %s line in /proc/stat", procStatBootTimeKey)
}
