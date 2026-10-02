package cli

import (
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/orchestra"
)

// Doctor connects to the device named by flags (--platform, --device,
// defaulting the same way the run command does) and reports its
// health: whether the connection succeeded and whether the device
// answers a bounded liveness check. It never runs a flow or touches
// app state.
//
// Exit codes are part of the command's contract: 0 when the device
// is reachable and healthy, 1 when the connection could not be
// established or the device is reachable but unhealthy, 2 when the
// command itself was misused (an unknown --platform value).
func Doctor(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	stdout := deps.stdout()
	stderr := deps.stderr()

	platform := resolvePlatform(flags, nil)
	if platform != "android" && platform != "ios" {
		_, _ = fmt.Fprintf(stderr, "error: Unknown platform %q. Use --platform android or --platform ios.\n", platform)
		return 2
	}
	deviceID := resolveDeviceID(flags, nil)
	jsonOutput := flagBool(flags, "--json")

	desc, err := resolveDescriptor(ctx, deps.Probe, platform, deviceID)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}
	built, err := deps.Factory.Build(ctx, desc)
	if err != nil {
		return reportUncaught(stderr, deps.Debug, err)
	}
	defer func() { _ = built.Driver.Disconnect() }()

	report := built.Orchestra.CheckHealth(ctx)

	if jsonOutput {
		if err := encodeJSON(stdout, report); err != nil {
			return reportUncaught(stderr, deps.Debug, err)
		}
	} else {
		printHealthReport(stdout, platform, desc.ID, report)
	}

	if !report.Healthy {
		return 1
	}
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
