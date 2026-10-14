package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/setup"
	"dev.atomyx/internal/shutdown"
)

const setupCommandName = "atomyx setup"

// Setup prepares the device named by flags (--platform, --device) to
// run Atomyx by executing the platform's setup steps in order. Without
// --check it applies the safe fixes the steps offer and prints exact
// instructions for the rest. With --check it only observes and never
// changes the machine or the device.
//
// Setup takes the device lease for the length of the run without
// connecting a driver, so a broken agent cannot fail inside a connect
// and no other Atomyx process drives the device while it is prepared.
// A device another process holds is reported with its holder, nothing
// is changed, and the command exits 1. The lease is released when the
// command ends, including when Ctrl-C interrupts it. When no device
// can be resolved, no lease is taken and the steps report what is
// missing.
//
// With --json the report is printed to standard output as
// {ok, steps:[{id, status, message, remedy}]}.
//
// Exit codes:
//
//   - 0 when every step is ok or fixed.
//   - 1 when any step is manual, failed or skipped, or the device is
//     held by another process.
//   - 2 for an unknown platform.
//   - The signal's conventional code (130 for SIGINT, 143 for SIGTERM)
//     when a stop signal ended the run.
func Setup(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	return runSetup(ctx, deps, flags, setupStepsByPlatform)
}

func runSetup(ctx context.Context, deps Deps, flags map[string]FlagValue, steps map[string][]setupStepFactory) int {
	stdout, stderr := deps.stdout(), deps.stderr()
	platform := resolvePlatform(flags, nil)
	if platform != "android" && platform != "ios" {
		_, _ = fmt.Fprintf(stderr, "error: Unknown platform %q. Use --platform android or --platform ios.\n", platform)
		return 2
	}
	jsonOutput := flagBool(flags, "--json")
	mode := setup.ModeFix
	if flagBool(flags, "--check") {
		mode = setup.ModeCheck
	}

	target := setupTarget{Platform: platform, DeviceID: resolveDeviceID(flags, nil), Deps: deps}
	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, target.DeviceID)
	if err != nil {
		_, message, _ := diag.Describe(err)
		_, _ = fmt.Fprintf(stderr, "note: no device was resolved, so no device lease is taken: %s\n", message)
	} else {
		target.DeviceID = desc.ID
		target.Kind = desc.Kind
		release, err := takeSetupLease(ctx, deps, desc)
		if err != nil {
			if holder, known, isHeld := heldHolder(err); isHeld {
				return reportSetupHeld(stdout, jsonOutput, platform, desc.ID, holder, known)
			}
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
		defer release()
	}

	registry := setup.NewRegistry()
	for _, build := range steps[platform] {
		entry := build(target)
		if err := registry.Add(entry.Step, entry.Requires...); err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
	}
	report := setup.Run(ctx, registry, mode)

	if jsonOutput {
		if err := report.WriteJSON(stdout); err != nil {
			return reportUncaught(ctx, stderr, deps.Debug, err)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "Atomyx: setup\n  platform: %s\n", platform)
		if target.DeviceID != "" {
			_, _ = fmt.Fprintf(stdout, "  device:   %s\n", target.DeviceID)
		}
		report.WriteText(stdout)
	}
	if code, ok := shutdown.InterruptExitCode(ctx); ok {
		_, _ = fmt.Fprintln(stderr, "interrupted")
		return code
	}
	return report.ExitCode()
}

// takeSetupLease claims the device lease for desc without building a
// driver. The returned release is safe to call once and gives the
// lease back. It is a no-op when the composition root configured no
// lease manager, which only a test does.
func takeSetupLease(ctx context.Context, deps Deps, desc device.Descriptor) (func(), error) {
	if deps.Session.Leases == nil {
		return func() {}, nil
	}
	identity, err := deps.Session.ResolveIdentity(ctx, desc)
	if err != nil {
		return nil, err
	}
	pid := deps.Session.PID
	if pid == 0 {
		pid = os.Getpid()
	}
	command := deps.Session.Command
	if command == "" {
		command = setupCommandName
	}
	held, err := deps.Session.Leases.Acquire(ctx, lease.KeyFor(string(identity.Platform), identity.HardwareID), lease.Holder{
		PID:       pid,
		Command:   command,
		Role:      "cli",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return func() { _ = held.Release() }, nil
}

// setupHeldJSON is the --json shape for a device another process
// holds: the setup report shape with no steps run, plus the holder.
type setupHeldJSON struct {
	OK     bool               `json:"ok"`
	Steps  []setup.StepReport `json:"steps"`
	Held   bool               `json:"held"`
	Holder *holderJSON        `json:"holder,omitempty"`
}

// reportSetupHeld reports a device another process drives. Setup
// changes nothing on such a device, so it exits 1: the device was not
// prepared.
func reportSetupHeld(stdout io.Writer, jsonOutput bool, platform, deviceID string, holder lease.Holder, known bool) int {
	var info *holderJSON
	if known {
		info = &holderJSON{PID: holder.PID, Command: holder.Command, Role: holder.Role, State: holder.State(), RunID: holder.RunID}
	}
	if jsonOutput {
		_ = encodeJSON(stdout, setupHeldJSON{OK: false, Steps: []setup.StepReport{}, Held: true, Holder: info})
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Atomyx: setup\n")
	_, _ = fmt.Fprintf(stdout, "  platform: %s\n", platform)
	_, _ = fmt.Fprintf(stdout, "  device:   %s\n", deviceID)
	_, _ = fmt.Fprintf(stdout, "  held:     true\n")
	if info != nil {
		_, _ = fmt.Fprintf(stdout, "  holder:   %s\n", holder.Describe())
	} else {
		_, _ = fmt.Fprintf(stdout, "  holder:   unknown process\n")
	}
	_, _ = fmt.Fprintf(stdout, "  nothing was changed because another Atomyx process is using the device\n")
	return 1
}
