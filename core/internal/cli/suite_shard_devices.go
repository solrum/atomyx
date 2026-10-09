package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/shard"
)

// shardRequested reports whether flags ask for a suite spread over several
// devices. A worker is never a parent.
func shardRequested(flags map[string]FlagValue) bool {
	if flagBool(flags, "--worker") {
		return false
	}
	devices, _ := flagString(flags, "--devices")
	mode, _ := flagString(flags, "--shard")
	return devices != "" || mode != ""
}

// shardRequest is which devices a sharded suite uses: every connected
// device of the resolved platform, or the devices named by id.
type shardRequest struct {
	auto bool
	ids  []string
}

// shardModeAuto is the only value --shard accepts.
const shardModeAuto = "auto"

// parseShardRequest reads --devices and --shard. A returned error is a
// usage error.
func parseShardRequest(flags map[string]FlagValue) (shardRequest, error) {
	if _, single := flagString(flags, "--device"); single {
		return shardRequest{}, errors.New("--device runs the suite on one device and cannot be combined with --devices or --shard")
	}
	list, hasList := flagString(flags, "--devices")
	mode, hasMode := flagString(flags, "--shard")
	switch {
	case hasList && hasMode:
		return shardRequest{}, errors.New("--devices and --shard cannot be combined, name the devices or use --shard auto")
	case hasMode:
		if mode != shardModeAuto {
			return shardRequest{}, fmt.Errorf("--shard accepts only %q, got %q", shardModeAuto, mode)
		}
		return shardRequest{auto: true}, nil
	}
	var ids []string
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return shardRequest{}, errors.New("--devices needs at least one device id, for example --devices emulator-5554,emulator-5556")
	}
	return shardRequest{ids: ids}, nil
}

// resolveShardDevices turns a request into the devices to start workers
// for. defaultPlatform is the platform the command resolved, which picks
// the devices of --shard auto and the platform of a named id the probe does
// not list, such as an Android device reached over the network. A returned
// error is a usage error.
func resolveShardDevices(ctx context.Context, probe device.Probe, req shardRequest, defaultPlatform string) ([]shard.Device, error) {
	var scanned []device.Descriptor
	if probe != nil {
		scanned = probe.Scan(ctx)
	}
	if req.auto {
		var devices []shard.Device
		for _, d := range scanned {
			if string(d.Platform) == defaultPlatform && (d.State == "" || d.State == device.StateOnline) {
				devices = append(devices, shard.Device{ID: d.ID, Platform: string(d.Platform)})
			}
		}
		if len(devices) == 0 {
			return nil, fmt.Errorf("--shard auto found no connected %s device that is ready. Connect a device, or name devices with --devices", defaultPlatform)
		}
		return devices, nil
	}
	devices := make([]shard.Device, len(req.ids))
	for i, id := range req.ids {
		devices[i] = shard.Device{ID: id, Platform: defaultPlatform}
		for _, d := range scanned {
			if strings.EqualFold(d.ID, id) {
				devices[i] = shard.Device{ID: d.ID, Platform: string(d.Platform)}
				break
			}
		}
	}
	return devices, nil
}

// workerForwardedValues are the single-value flags a sharded suite passes on
// to every worker so the worker reads the same project, environment and
// policy.
var workerForwardedValues = []string{"--config", "--env", "--var-file", "--preconditions", "--report-dir", "--retries"}

// workerForwardedSwitches are the boolean flags passed on to every worker.
var workerForwardedSwitches = []string{"--skip-preconditions", "--video"}

// workerArgs builds the command line of the worker that drives device for
// the sharding run parentRunID. The entries themselves arrive on the
// worker's input, so the selector flags are not forwarded.
func workerArgs(positional *string, flags map[string]FlagValue, device shard.Device, parentRunID string) []string {
	args := []string{"suite"}
	if positional != nil {
		args = append(args, *positional)
	}
	args = append(args, "--device", device.ID, "--platform", device.Platform, "--worker", "--parent-run", parentRunID)
	for _, name := range workerForwardedValues {
		if value, ok := flagString(flags, name); ok {
			args = append(args, name, value)
		}
	}
	for _, value := range flagStrings(flags, "--var") {
		args = append(args, "--var", value)
	}
	for _, name := range workerForwardedSwitches {
		if flagBool(flags, name) {
			args = append(args, name)
		}
	}
	return args
}
