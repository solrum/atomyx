package androiddriver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dev.atomyx/internal/process"
)

// adbForward is one entry from "adb forward --list": a serial, the
// host-side endpoint, and the device-side endpoint it forwards to.
type adbForward struct {
	serial string
	local  string
	remote string
}

// listAdbForwards runs "adb forward --list" and parses every forward
// currently registered with adb, across every connected device. A
// host-side local endpoint is a real listening socket on this
// workstation, so at most one forward can hold a given local endpoint
// at a time, regardless of which serial originally created it.
func listAdbForwards(ctx context.Context, proc process.Runner) ([]adbForward, error) {
	result, err := proc.Run(ctx, "adb", "forward", "--list")
	if err != nil {
		return nil, err
	}
	var forwards []adbForward
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		forwards = append(forwards, adbForward{serial: fields[0], local: fields[1], remote: fields[2]})
	}
	return forwards, nil
}

// findAdbForward returns the forward already bound to localPort, if
// any. A local endpoint is a single listening socket, so at most one
// entry can ever match.
func findAdbForward(forwards []adbForward, localPort string) (adbForward, bool) {
	for _, f := range forwards {
		if f.local == localPort {
			return f, true
		}
	}
	return adbForward{}, false
}

const maxPort = 65535

// findSerialForward returns the host port of a forward that already
// maps serial to remote, if any.
func findSerialForward(forwards []adbForward, serial, remote string) (int, bool) {
	for _, f := range forwards {
		if f.serial != serial || f.remote != remote || !strings.HasPrefix(f.local, "tcp:") {
			continue
		}
		port, err := strconv.Atoi(strings.TrimPrefix(f.local, "tcp:"))
		if err != nil || port <= 0 || port > maxPort {
			continue
		}
		return port, true
	}
	return 0, false
}

// parseAllocatedPort reads the host port "adb forward tcp:0 ..."
// prints on stdout. Empty or non-numeric output is an error rather
// than a guess, because a wrong port would silently talk to something
// else.
func parseAllocatedPort(stdout string) (int, error) {
	text := strings.TrimSpace(stdout)
	port, err := strconv.Atoi(text)
	if err != nil || port <= 0 || port > maxPort {
		return 0, fmt.Errorf("androiddriver: adb forward tcp:0 printed %q, want a host port number", text)
	}
	return port, nil
}

// ForwardHostPort returns the host port that reaches devicePort on
// serial. A forward adb already holds for that serial and device port
// is reused, and otherwise adb allocates a fresh host port, so no
// device is tied to a fixed host port another device could claim.
func ForwardHostPort(ctx context.Context, proc process.Runner, serial string, devicePort int) (int, error) {
	remote := fmt.Sprintf("tcp:%d", devicePort)
	forwards, err := listAdbForwards(ctx, proc)
	if err != nil {
		return 0, fmt.Errorf("androiddriver: adb forward --list: %w", err)
	}
	if port, found := findSerialForward(forwards, serial, remote); found {
		return port, nil
	}
	result, err := proc.Run(ctx, "adb", "-s", serial, "forward", "tcp:0", remote)
	if err != nil {
		return 0, fmt.Errorf("androiddriver: adb forward tcp:0: %w", err)
	}
	return parseAllocatedPort(result.Stdout)
}
