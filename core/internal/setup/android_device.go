package setup

import "context"

// The states a DeviceStateReader reports for a device adb lists, and the
// empty state it reports for a device adb does not list.
const (
	// DeviceOnline means adb lists the device in the device state.
	DeviceOnline = "online"
	// DeviceOffline means adb lists the device but cannot reach it.
	DeviceOffline = "offline"
	// DeviceUnauthorized means the device has not accepted this host
	// for USB debugging.
	DeviceUnauthorized = "unauthorized"
	// DeviceUnavailable means the device is in the bootloader, recovery
	// or sideload mode, so no Android shell answers.
	DeviceUnavailable = "unavailable"
	// DeviceAbsent means adb does not list the device at all.
	DeviceAbsent = ""
)

// DeviceStateReader reports how adb sees an Android device.
type DeviceStateReader interface {
	// DeviceState reports the state of the device with the given
	// serial as one of the Device constants, which is DeviceAbsent
	// when adb does not list it.
	DeviceState(ctx context.Context, serial string) string
}

// AndroidDeviceStepID is the stable identifier the step reports.
const AndroidDeviceStepID = "android-device"

type androidDeviceStep struct {
	serial string
	reader DeviceStateReader
}

// NewAndroidDeviceStep returns the step that requires the resolved
// device to be listed by adb in the device state. An empty serial
// means no device was resolved.
func NewAndroidDeviceStep(serial string, reader DeviceStateReader) Step {
	return androidDeviceStep{serial: serial, reader: reader}
}

func (androidDeviceStep) ID() string { return AndroidDeviceStepID }

func (s androidDeviceStep) Check(ctx context.Context) Result {
	if s.serial == "" {
		return Result{
			Status:  StatusManual,
			Message: "no Android device was resolved",
			Remedy:  "Connect a device with USB debugging enabled or start an emulator, run atomyx devices to see what is available, then pass --device <serial>.",
		}
	}
	switch s.reader.DeviceState(ctx, s.serial) {
	case DeviceOnline:
		return Result{Status: StatusOK, Message: "device " + s.serial + " is connected"}
	case DeviceUnauthorized:
		return Result{
			Status:  StatusManual,
			Message: "device " + s.serial + " has not authorized this computer for USB debugging",
			Remedy:  "Unlock the device, accept the USB debugging prompt, then run atomyx setup again.",
		}
	case DeviceOffline:
		return Result{
			Status:  StatusManual,
			Message: "device " + s.serial + " is listed by adb but offline",
			Remedy:  "Reconnect the USB cable, or restart the adb server with adb kill-server and adb start-server, then run atomyx setup again.",
		}
	case DeviceUnavailable:
		return Result{
			Status:  StatusManual,
			Message: "device " + s.serial + " is in the bootloader, recovery or sideload mode",
			Remedy:  "Boot the device into Android, then run atomyx setup again.",
		}
	default:
		return Result{
			Status:  StatusManual,
			Message: "device " + s.serial + " is not listed by adb devices",
			Remedy:  "Connect the device with USB debugging enabled, check the serial with atomyx devices, then run atomyx setup again.",
		}
	}
}
