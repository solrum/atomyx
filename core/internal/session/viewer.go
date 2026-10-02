package session

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/wire"
)

// ImageFormat names the encoding of a screenshot, read from its bytes.
type ImageFormat string

const (
	// ImageUnknown is reported when the bytes are not a PNG or a JPEG,
	// or when no screenshot has been captured.
	ImageUnknown ImageFormat = ""
	ImagePNG     ImageFormat = "png"
	ImageJPEG    ImageFormat = "jpeg"
)

// ObservationKind says which capture a notification announces.
type ObservationKind int

const (
	ObservedTree ObservationKind = iota + 1
	ObservedScreenshot
	ObservedScreenSize
)

// Observation announces that a new capture is available. It carries no
// payload: a subscriber reads the value with Latest, so a slow reader
// only ever sees the newest capture and never a backlog.
type Observation struct {
	Kind  ObservationKind
	Epoch uint64
}

// Snapshot is the newest capture of each kind, as a viewer may show it.
// Callers read fields by name, so fields added later never break them.
// Every slice and map in it is shared and must be treated as read-only.
type Snapshot struct {
	// Tree is the newest hierarchy with every secret masked. It is the
	// zero value while HasTree is false.
	Tree    wire.TreeNodeWire
	HasTree bool
	// TreeEpoch is the action epoch the tree was fetched in.
	TreeEpoch uint64

	// Screenshot holds the encoded image of the newest screenshot.
	Screenshot    []byte
	HasScreenshot bool
	// ScreenshotFormat is read from the image bytes.
	ScreenshotFormat ImageFormat
	// ScreenshotEpoch is the action epoch the screenshot was fetched in.
	ScreenshotEpoch uint64

	// Scale is the screenshot pixel width divided by the screen width
	// the driver reports. It is zero while either is unknown.
	Scale float64
	// Paired reports that the tree and the screenshot were fetched in
	// the same action epoch, so they describe the same screen.
	Paired bool
	// Highlightable reports that a tree node's bounds can be drawn on
	// the screenshot: the pair matches and a scale is known.
	Highlightable bool
}

// Info describes who holds the device and what they are doing. The
// state comes from the lease metadata, so it is "idle" or "running"
// while the session is live, and "lost" or "closed" afterwards.
type Info struct {
	Device device.Descriptor
	Holder lease.Holder
	Role   string
	State  string
}

// Viewer reads what a session has captured. It never calls the device,
// so any number of viewers cost the holder nothing.
type Viewer interface {
	// Info reports the holder, its role and its state.
	Info() Info
	// Latest returns the newest capture of each kind. The tree has its
	// secrets masked.
	Latest() Snapshot
	// Subscribe reports every new capture until ctx ends or the
	// subscriber falls behind, and then closes the channel.
	Subscribe(ctx context.Context) (<-chan Observation, error)
}
