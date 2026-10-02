package process

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"
)

func inspectProcess(pid int) (ProcessInfo, error) {
	statRaw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if os.IsNotExist(err) {
			return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, ErrProcessNotFound)
		}
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, err)
	}
	rec, err := parseProcStat(string(statRaw))
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("inspect process %d: %w", pid, err)
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
		PGID:      rec.pgid,
		UID:       uid,
		Command:   rec.command,
		StartTime: procStartTime(boot, rec.startTicks),
	}, nil
}

func hostBootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read the host boot time: %w", err)
	}
	return parseProcStatBootTime(string(data))
}

func ownProcessGroup() int { return syscall.Getpgrp() }
