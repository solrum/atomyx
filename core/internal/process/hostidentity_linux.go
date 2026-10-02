package process

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procStatBootTimeKey names the line of /proc/stat that holds the boot
// time as seconds since the epoch.
const procStatBootTimeKey = "btime"

// procStatStartTimeField is the 1-based index of the start time field
// in /proc/<pid>/stat, in clock ticks since boot, and procStatPgrpField
// is the process group field. Both count from the field after the
// parenthesized command.
const (
	procStatPgrpField      = 3
	procStatStartTimeField = 20
)

// clockTicksPerSecond is the unit of the /proc start time. It is 100 on
// every Linux configuration Atomyx supports.
const clockTicksPerSecond = 100

func inspectProcess(pid int) (ProcessInfo, error) {
	statRaw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if os.IsNotExist(err) {
			return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
		}
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, err)
	}
	stat := string(statRaw)
	open, closeIdx := strings.IndexByte(stat, '('), strings.LastIndexByte(stat, ')')
	if open < 0 || closeIdx < open {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: malformed stat record", pid)
	}
	command := stat[open+1 : closeIdx]
	fields := strings.Fields(stat[closeIdx+1:])
	if len(fields) <= procStatStartTimeField {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: short stat record", pid)
	}
	pgid, err := strconv.Atoi(fields[procStatPgrpField])
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: parse the process group: %w", pid, err)
	}
	ticks, err := strconv.ParseInt(fields[procStatStartTimeField], 10, 64)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: parse the start time: %w", pid, err)
	}
	boot, err := hostBootTime()
	if err != nil {
		return ProcessInfo{}, err
	}
	info, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
	}
	uid := 0
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		uid = int(st.Uid)
	}
	return ProcessInfo{
		PID:       pid,
		PGID:      pgid,
		UID:       uid,
		Command:   command,
		StartTime: boot.Add(time.Duration(ticks) * time.Second / clockTicksPerSecond),
	}, nil
}

func hostBootTime() (time.Time, error) {
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

func ownProcessGroup() int { return syscall.Getpgrp() }
