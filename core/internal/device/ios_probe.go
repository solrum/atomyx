package device

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"dev.atomyx/internal/process"
)

// IosProbe scans iOS simulators via `xcrun simctl` and physical
// devices via `idevice_id` / `idevicename` (libimobiledevice). A
// missing xcrun (non-macOS) or a missing libimobiledevice tool is
// treated as no devices found for that half of the scan rather than
// an error, so the other half, including the Android probe, still
// produces results.
//
// Only devices the host can currently talk to are reported: a
// shutdown simulator is installed but not reachable, and listing it
// alongside booted simulators would make a device picker list dozens
// of inert choices. Its simulator name carries a " · <runtime>"
// suffix and its result orders booted simulators first, both to make
// a device picker usable when several simulators are reachable at
// once. The CLI's list-devices command renders devices for a human
// to read rather than to pick from, so it uses
// IosBootedSimulatorProbe instead, which skips both.
type IosProbe struct {
	runner process.Runner
}

// NewIosProbe constructs an IosProbe that shells out through runner.
func NewIosProbe(runner process.Runner) *IosProbe {
	return &IosProbe{runner: runner}
}

var (
	_ Probe          = (*IosProbe)(nil)
	_ PlatformScoped = (*IosProbe)(nil)
)

// ProbePlatform implements PlatformScoped.
func (*IosProbe) ProbePlatform() Platform { return PlatformIOS }

// Scan implements Probe.
func (p *IosProbe) Scan(ctx context.Context) []Descriptor {
	entries := p.listDevices(ctx)
	out := make([]Descriptor, 0, len(entries))
	for _, e := range entries {
		if !iosReachable(e.state) {
			continue
		}
		name := e.name
		if e.runtime != "" {
			name = e.name + " · " + e.runtime
		}
		kind := KindDevice
		if e.kind == "simulator" {
			kind = KindSimulator
		}
		out = append(out, Descriptor{
			ID:       e.udid,
			Platform: PlatformIOS,
			Name:     name,
			Kind:     kind,
			State:    StateOnline,
		})
	}
	return out
}

// iosReachable reports whether a simctl "Booted" simulator or an
// idevice_id "device" physical entry is addressable right now.
// Every other simctl state (Shutdown, Booting, ShuttingDown) is
// skipped.
func iosReachable(state string) bool {
	return state == "Booted" || state == "device"
}

// iosDeviceEntry is one simulator or physical device found by
// listDevices, before the reachability filter in Scan.
type iosDeviceEntry struct {
	udid    string
	name    string
	kind    string // "simulator" | "device"
	state   string
	runtime string
}

func (p *IosProbe) listDevices(ctx context.Context) []iosDeviceEntry {
	sims := sortBootedFirst(p.listSimulators(ctx))
	phys := listPhysicalIosDevices(ctx, p.runner)
	out := make([]iosDeviceEntry, 0, len(sims)+len(phys))
	out = append(out, sims...)
	out = append(out, phys...)
	return out
}

type simctlDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
}

type simctlListOutput struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

var simRuntimePrefix = regexp.MustCompile(`^com\.apple\.CoreSimulator\.SimRuntime\.`)

func (p *IosProbe) listSimulators(ctx context.Context) []iosDeviceEntry {
	result, err := p.runner.Run(ctx, "xcrun", "simctl", "list", "devices", "--json")
	if err != nil {
		return nil
	}
	return parseSimctlOutput(result.Stdout)
}

// parseSimctlOutput parses `xcrun simctl list devices --json`. Only
// devices simctl reports as available are returned; the runtime
// identifier prefix common to every entry is stripped to a short
// label (e.g. "iOS-17-4").
func parseSimctlOutput(raw string) []iosDeviceEntry {
	var parsed simctlListOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	var out []iosDeviceEntry
	for runtime, entries := range parsed.Devices {
		label := simRuntimePrefix.ReplaceAllString(runtime, "")
		for _, e := range entries {
			if !e.IsAvailable {
				continue
			}
			out = append(out, iosDeviceEntry{
				udid:    e.UDID,
				name:    e.Name,
				kind:    "simulator",
				state:   e.State,
				runtime: label,
			})
		}
	}
	return out
}

// listPhysicalIosDevices lists paired physical iOS devices via
// `idevice_id -l`. Shared by IosProbe and IosBootedSimulatorProbe:
// both report physical devices identically, so only the simulator
// half of the scan differs between them.
func listPhysicalIosDevices(ctx context.Context, runner process.Runner) []iosDeviceEntry {
	result, err := runner.Run(ctx, "idevice_id", "-l")
	if err != nil {
		return nil
	}
	out := make([]iosDeviceEntry, 0)
	for _, line := range strings.Split(result.Stdout, "\n") {
		udid := strings.TrimSpace(line)
		if udid == "" {
			continue
		}
		out = append(out, iosDeviceEntry{
			udid:  udid,
			name:  physicalIosDeviceName(ctx, runner, udid),
			kind:  "device",
			state: "device",
		})
	}
	return out
}

// physicalIosDeviceName resolves the display name for udid via
// `idevicename`, falling back to the udid itself when the tool is
// missing, fails, or reports a blank name.
func physicalIosDeviceName(ctx context.Context, runner process.Runner, udid string) string {
	result, err := runner.Run(ctx, "idevicename", "-u", udid)
	if err != nil {
		return udid
	}
	name := strings.TrimSpace(result.Stdout)
	if name == "" {
		return udid
	}
	return name
}

// IosBootedSimulatorProbe scans already-booted iOS simulators via
// `xcrun simctl list devices booted` and physical devices the same
// way IosProbe does. Unlike IosProbe, it reports a booted simulator
// exactly as simctl names it, with no runtime-label suffix, no
// availability filter, and no reordering: this is the scan behind the
// CLI's list-devices command, which renders devices for a human to
// read rather than feeding a device-selection port, so it carries
// none of IosProbe's picker-usability behavior.
type IosBootedSimulatorProbe struct {
	runner process.Runner
}

// NewIosBootedSimulatorProbe constructs an IosBootedSimulatorProbe
// that shells out through runner.
func NewIosBootedSimulatorProbe(runner process.Runner) *IosBootedSimulatorProbe {
	return &IosBootedSimulatorProbe{runner: runner}
}

var (
	_ Probe          = (*IosBootedSimulatorProbe)(nil)
	_ PlatformScoped = (*IosBootedSimulatorProbe)(nil)
)

// ProbePlatform implements PlatformScoped.
func (*IosBootedSimulatorProbe) ProbePlatform() Platform { return PlatformIOS }

// Scan implements Probe.
func (p *IosBootedSimulatorProbe) Scan(ctx context.Context) []Descriptor {
	sims := p.listBootedSimulators(ctx)
	phys := listPhysicalIosDevices(ctx, p.runner)
	out := make([]Descriptor, 0, len(sims)+len(phys))
	for _, e := range sims {
		out = append(out, Descriptor{ID: e.udid, Platform: PlatformIOS, Name: e.name, Kind: KindSimulator, State: StateOnline})
	}
	for _, e := range phys {
		out = append(out, Descriptor{ID: e.udid, Platform: PlatformIOS, Name: e.name, Kind: KindDevice, State: StateOnline})
	}
	return out
}

func (p *IosBootedSimulatorProbe) listBootedSimulators(ctx context.Context) []iosDeviceEntry {
	result, err := p.runner.Run(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json")
	if err != nil {
		return nil
	}
	return parseBootedSimctlOutput(result.Stdout)
}

// parseBootedSimctlOutput parses `xcrun simctl list devices booted
// --json`: simctl itself has already restricted the result to booted
// simulators, so every entry is included as-is, with no availability
// filter and no runtime-label suffix on its name. Runtimes are
// visited in sorted key order for a deterministic result: Go map
// iteration order is randomized per run, unlike the insertion-order
// object iteration a JavaScript JSON.parse result preserves.
func parseBootedSimctlOutput(raw string) []iosDeviceEntry {
	var parsed simctlListOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	runtimes := make([]string, 0, len(parsed.Devices))
	for runtime := range parsed.Devices {
		runtimes = append(runtimes, runtime)
	}
	sort.Strings(runtimes)

	var out []iosDeviceEntry
	for _, runtime := range runtimes {
		for _, e := range parsed.Devices[runtime] {
			out = append(out, iosDeviceEntry{udid: e.UDID, name: e.Name, kind: "simulator", state: e.State})
		}
	}
	return out
}

// sortBootedFirst orders entries with any "Booted" simulator first,
// then alphabetically by name. A stable sort keeps the relative
// order of entries with an equal state and name.
func sortBootedFirst(entries []iosDeviceEntry) []iosDeviceEntry {
	out := append([]iosDeviceEntry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.state == b.state {
			return a.name < b.name
		}
		if a.state == "Booted" {
			return true
		}
		if b.state == "Booted" {
			return false
		}
		return a.name < b.name
	})
	return out
}
