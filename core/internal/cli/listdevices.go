package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"dev.atomyx/internal/device"
)

// deviceEntry is the JSON shape of one entry in list-devices' device
// list. Field order is part of the --json output's contract and must
// not change incidentally.
type deviceEntry struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Kind     string `json:"kind"`
	// Lease is the device's lease as a read-only probe sees it: free,
	// held with the holder's command and state, or unknown when the
	// probe failed.
	Lease bundleLeaseJSON `json:"lease"`
}

// devicesEnvelope is the JSON shape list-devices --json writes.
// Warnings is always empty: device.Probe reports a scan tool that
// could not run (adb missing, xcrun missing) by returning fewer
// devices rather than an error, so there is nothing to carry in this
// field yet.
type devicesEnvelope struct {
	Devices  []deviceEntry `json:"devices"`
	Warnings []string      `json:"warnings"`
}

// ListDevices scans for connected devices through deps.Probe and
// renders them to w, as JSON when jsonOutput is set or as the grouped
// text listing otherwise. Each device carries its lease, probed
// read-only, so a device another Atomyx process drives shows that
// holder and a failed probe shows as unknown. A scan tool that could
// not run is reflected as fewer devices found, never a command
// failure, so the only way this returns a non-zero exit code is a
// write failure while encoding the JSON envelope to w.
func ListDevices(ctx context.Context, deps Deps, jsonOutput bool, w io.Writer) int {
	descriptors := deps.Probe.Scan(ctx)
	leases := make(map[string]bundleLeaseJSON, len(descriptors))
	for _, d := range descriptors {
		leases[deviceKey(d)] = probeLease(ctx, deps, d)
	}

	if jsonOutput {
		entries := make([]deviceEntry, len(descriptors))
		for i, d := range descriptors {
			entries[i] = deviceEntry{
				Platform: string(d.Platform),
				ID:       d.ID,
				Name:     d.Name,
				State:    displayState(d),
				Kind:     string(d.Kind),
				Lease:    leases[deviceKey(d)],
			}
		}
		envelope := devicesEnvelope{Devices: entries, Warnings: []string{}}
		if err := encodeJSON(w, envelope); err != nil {
			return 1
		}
		return 0
	}

	renderDevicesText(w, descriptors, leases)
	return 0
}

func deviceKey(d device.Descriptor) string {
	return string(d.Platform) + "/" + d.ID
}

func renderDevicesText(w io.Writer, descriptors []device.Descriptor, leases map[string]bundleLeaseJSON) {
	_, _ = io.WriteString(w, "Atomyx: connected devices\n\n")

	android := filterDescriptors(descriptors, func(d device.Descriptor) bool {
		return d.Platform == device.PlatformAndroid
	})
	_, _ = io.WriteString(w, "Android (via adb):\n")
	writeDeviceGroup(w, android, true, leases)

	_, _ = io.WriteString(w, "\n")

	iosSims := filterDescriptors(descriptors, func(d device.Descriptor) bool {
		return d.Platform == device.PlatformIOS && d.Kind == device.KindSimulator
	})
	_, _ = io.WriteString(w, "iOS Simulators (booted):\n")
	writeDeviceGroup(w, iosSims, false, leases)

	_, _ = io.WriteString(w, "\n")

	iosDevices := filterDescriptors(descriptors, func(d device.Descriptor) bool {
		return d.Platform == device.PlatformIOS && d.Kind == device.KindDevice
	})
	_, _ = io.WriteString(w, "iOS Devices (USB):\n")
	writeDeviceGroup(w, iosDevices, false, leases)
}

// writeDeviceGroup writes one device-list group's lines. When
// nameOptional is true (the Android group), a device whose name
// equals its id omits the trailing name, because adb reports no
// separate model for some devices and simply repeats the serial as
// the name: printing it twice would be redundant.
func writeDeviceGroup(w io.Writer, descriptors []device.Descriptor, nameOptional bool, leases map[string]bundleLeaseJSON) {
	if len(descriptors) == 0 {
		_, _ = io.WriteString(w, "  (none)\n")
		return
	}
	for _, d := range descriptors {
		state := displayState(d)
		note := leaseNote(leases[deviceKey(d)])
		if nameOptional && d.Name == d.ID {
			_, _ = fmt.Fprintf(w, "  %s  [%s]%s\n", d.ID, state, note)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s  [%s] %s%s\n", d.ID, state, d.Name, note)
	}
}

// leaseNote is the text a device line ends with for its lease: nothing
// for a free device, the holder's command and state for a held one, and
// a plain statement when the probe failed.
func leaseNote(l bundleLeaseJSON) string {
	switch l.State {
	case "held":
		if l.Holder == nil {
			return "  held by an unknown process"
		}
		return fmt.Sprintf("  held by %s, %s", l.Holder.Command, l.Holder.State)
	case "unknown":
		return "  lease unknown"
	default:
		return ""
	}
}

// displayState renders a Descriptor's reachability for list-devices
// output using vocabulary distinct from the internal device.State
// enum, and this wording is part of the command's user-facing output
// contract: a reachable Android device reads "ready", a reachable iOS
// simulator reads "booted", and a reachable physical iOS device reads
// "connected". An Android device that is not reachable keeps its
// device.State literal ("offline" or "unauthorized") unchanged,
// passing the raw adb state straight through for anything other than
// "device".
func displayState(d device.Descriptor) string {
	switch d.Platform {
	case device.PlatformIOS:
		if d.Kind == device.KindSimulator {
			return "booted"
		}
		return "connected"
	case device.PlatformAndroid:
		if d.State == device.StateOnline {
			return "ready"
		}
		return string(d.State)
	default:
		return string(d.State)
	}
}

func filterDescriptors(descriptors []device.Descriptor, keep func(device.Descriptor) bool) []device.Descriptor {
	out := make([]device.Descriptor, 0, len(descriptors))
	for _, d := range descriptors {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

// encodeJSON writes v to w as one pretty-printed JSON blob followed
// by a trailing newline, matching JSON.stringify(v, null, 2) + "\n":
// two-space indentation, a space after every colon, and no HTML
// escaping of "<", ">", or "&". It returns the write error unchanged
// so a caller writing to a pipe that closed early (a broken stdout)
// can report the command as failed instead of exiting 0 on a partial
// write.
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
