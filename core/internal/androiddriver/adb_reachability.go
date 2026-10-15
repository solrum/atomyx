package androiddriver

import "strings"

// adbDeviceUnreachable reports whether adb output says the device is
// offline, unauthorized, or not attached, as opposed to the device
// running a command that failed.
func adbDeviceUnreachable(output string) bool {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "device offline") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "no devices/emulators found") {
		return true
	}
	return strings.Contains(lower, "device") && strings.Contains(lower, "not found")
}
