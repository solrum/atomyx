package cli

import (
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/orchestra"
)

// Doctor connects to the device named by flags (--platform, --device,
// defaulting the same way the run command does) and reports its
// health: whether the connection succeeded and whether the device
// answers a bounded liveness check. It never runs a flow or touches
// app state.
//
// A device another Atomyx process holds is not probed: doctor reports
// the holder and that health was not checked, and exits 0, because the
// holder proves something is driving the device.
//
// Exit codes are part of the command's contract: 0 when the device
// is reachable and healthy or held by another process, 1 when the connection could not be
// established or the device is reachable but unhealthy, 2 when the
// command itself was misused (an unknown --platform value).
//
// --report-dir is accepted only with --migrate-runs and is refused with
// exit 2 otherwise.
//
// With --migrate-runs, Doctor instead rewrites the flat run records
// under the run root as run folders and never touches a device. Its
// exit codes are documented on migrateRuns.
func Doctor(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	if flagBool(flags, migrateRunsFlag) {
		return migrateRuns(ctx, deps, flags)
	}
	stdout := deps.stdout()
	stderr := deps.stderr()
	if _, set := flags["--report-dir"]; set {
		_, _ = fmt.Fprintf(stderr, "error: --report-dir only applies with %s, because a device health check writes no report.\n", migrateRunsFlag)
		return 2
	}

	platform := resolvePlatform(flags, nil)
	if platform != "android" && platform != "ios" {
		_, _ = fmt.Fprintf(stderr, "error: Unknown platform %q. Use --platform android or --platform ios.\n", platform)
		return 2
	}
	deviceID := resolveDeviceID(flags, nil)
	jsonOutput := flagBool(flags, "--json")

	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, deviceID)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	opened, err := openDevice(ctx, deps, "atomyx doctor", desc)
	if err != nil {
		if holder, known, held := heldHolder(err); held {
			return reportDeviceHeld(stdout, jsonOutput, platform, desc.ID, holder, known)
		}
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	defer opened.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	opened.cancelOnLost(cancelRun)

	report := opened.Orchestra().CheckHealth(runCtx)

	if jsonOutput {
		if err := encodeJSON(stdout, doctorJSON{HealthReport: report, HealthChecked: true}); err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
	} else {
		printHealthReport(stdout, platform, desc.ID, report)
	}

	if !report.Healthy {
		return 1
	}
	return 0
}

// doctorJSON is the --json shape of doctor. It extends the health report
// with whether another process held the device, in which case no health
// check ran.
type doctorJSON struct {
	orchestra.HealthReport
	Held          bool        `json:"held"`
	HealthChecked bool        `json:"healthChecked"`
	Holder        *holderJSON `json:"holder,omitempty"`
}

// holderJSON is the display metadata of the process holding a device.
type holderJSON struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Role    string `json:"role"`
	State   string `json:"state"`
	RunID   string `json:"runId,omitempty"`
}

// reportDeviceHeld reports a device another process drives. A held
// device is a normal answer for a health command, not a failure, since
// the holder is the proof that something is using it, so it exits 0.
func reportDeviceHeld(stdout io.Writer, jsonOutput bool, platform, deviceID string, holder lease.Holder, known bool) int {
	var info *holderJSON
	if known {
		info = &holderJSON{PID: holder.PID, Command: holder.Command, Role: holder.Role, State: holder.State(), RunID: holder.RunID}
	}
	if jsonOutput {
		if err := encodeJSON(stdout, doctorJSON{Held: true, HealthChecked: false, Holder: info}); err != nil {
			return 1
		}
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "Atomyx: device health\n")
	_, _ = fmt.Fprintf(stdout, "  platform: %s\n", platform)
	_, _ = fmt.Fprintf(stdout, "  device:   %s\n", deviceID)
	_, _ = fmt.Fprintf(stdout, "  held:     true\n")
	if info != nil {
		_, _ = fmt.Fprintf(stdout, "  holder:   %s\n", holder.Describe())
	} else {
		_, _ = fmt.Fprintf(stdout, "  holder:   unknown process\n")
	}
	_, _ = fmt.Fprintf(stdout, "  health was not checked because another Atomyx process is using the device\n")
	return 0
}

// printHealthReport renders report as text to w, naming the platform
// and device id the check ran against so a caller reading the output
// alone can tell which device it describes.
func printHealthReport(w io.Writer, platform, deviceID string, report orchestra.HealthReport) {
	_, _ = fmt.Fprintf(w, "Atomyx: device health\n")
	_, _ = fmt.Fprintf(w, "  platform: %s\n", platform)
	_, _ = fmt.Fprintf(w, "  device:   %s\n", deviceID)
	if report.Healthy {
		_, _ = fmt.Fprintf(w, "  healthy:  true\n")
		return
	}
	_, _ = fmt.Fprintf(w, "  healthy:  false\n")
	_, _ = fmt.Fprintf(w, "  code:     %s\n", report.Code)
	_, _ = fmt.Fprintf(w, "  message:  %s\n", report.Message)
	if report.Remedy != "" {
		_, _ = fmt.Fprintf(w, "  remedy:   %s\n", report.Remedy)
	}
}
