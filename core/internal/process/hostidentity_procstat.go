package process

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// procStatBootTimeKey names the line of /proc/stat that holds the boot
// time as seconds since the epoch.
const procStatBootTimeKey = "btime"

// Field numbers of /proc/<pid>/stat as numbered in proc(5), starting at
// 1 for the pid. The command in field 2 may contain spaces and
// parentheses, so fields are counted from the text after its closing
// parenthesis, where field 3 (the state) comes first.
const (
	procStatPgrpField      = 5
	procStatStartTimeField = 22
	procStatFirstTailField = 3
)

// clockTicksPerSecond is the unit of the /proc start time. It is 100 on
// every Linux configuration Atomyx supports.
const clockTicksPerSecond = 100

// procStartTime converts a start time in clock ticks since boot to a
// wall-clock time.
func procStartTime(boot time.Time, ticks int64) time.Time {
	return boot.Add(time.Duration(ticks) * time.Second / clockTicksPerSecond)
}

type procStatRecord struct {
	command string
	pgid    int
	// startTicks is the start time in clock ticks since boot.
	startTicks int64
}

// parseProcStat reads the command, process group, and start time from
// the text of /proc/<pid>/stat.
func parseProcStat(stat string) (procStatRecord, error) {
	open, closeIdx := strings.IndexByte(stat, '('), strings.LastIndexByte(stat, ')')
	if open < 0 || closeIdx < open {
		return procStatRecord{}, fmt.Errorf("malformed stat record")
	}
	tail := strings.Fields(stat[closeIdx+1:])
	startIdx := procStatStartTimeField - procStatFirstTailField
	if len(tail) <= startIdx {
		return procStatRecord{}, fmt.Errorf("short stat record")
	}
	pgid, err := strconv.Atoi(tail[procStatPgrpField-procStatFirstTailField])
	if err != nil {
		return procStatRecord{}, fmt.Errorf("parse the process group: %w", err)
	}
	ticks, err := strconv.ParseInt(tail[startIdx], 10, 64)
	if err != nil {
		return procStatRecord{}, fmt.Errorf("parse the start time: %w", err)
	}
	return procStatRecord{command: stat[open+1 : closeIdx], pgid: pgid, startTicks: ticks}, nil
}

// parseProcStatBootTime reads the host boot time from the text of
// /proc/stat.
func parseProcStatBootTime(procStat string) (time.Time, error) {
	for _, line := range strings.Split(procStat, "\n") {
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
