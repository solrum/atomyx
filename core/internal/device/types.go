package device

// Platform identifies the device family a Descriptor belongs to.
type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

// Kind classifies the physical nature of a device.
type Kind string

const (
	KindSimulator Kind = "simulator"
	KindEmulator  Kind = "emulator"
	KindDevice    Kind = "device"
)

// State reports whether a device is currently reachable.
type State string

const (
	StateOnline       State = "online"
	StateOffline      State = "offline"
	StateUnauthorized State = "unauthorized"
	// StateUnavailable reports a device adb can see but cannot yet
	// drive for a reason that has nothing to do with USB debugging
	// authorization: the bootloader, recovery, and sideload adb
	// states all land here rather than under StateUnauthorized.
	StateUnavailable State = "unavailable"
)

// Descriptor identifies one device a Probe found reachable from the
// current host.
type Descriptor struct {
	ID       string   `json:"id"`
	Platform Platform `json:"platform"`
	Name     string   `json:"name"`
	Kind     Kind     `json:"kind"`
	State    State    `json:"state"`
	// Port overrides the driver's default control-channel port. Zero
	// means "use the platform default"; a Factory honors this only
	// for platforms whose driver exposes a caller-facing port option.
	Port int `json:"-"`

	// HidPort is the atomyx-sim-hid helper's WebSocket handshake
	// port. Non-zero only for an iOS simulator descriptor that has
	// already opted into the direct-HID path and started the helper;
	// a Factory that sees it set constructs an iossimdriver.IosSimDriver
	// instead of the XCUITest-only iosdriver.IosDriver. Starting the
	// helper and deciding whether to opt in are the caller's
	// responsibility, not this package's.
	HidPort int `json:"-"`
}
