package diag

import "fmt"

// NewUnsupported builds the CodeUnsupported error for a message type the
// receiver does not implement.
func NewUnsupported(messageType string) error {
	return New(
		CodeUnsupported,
		fmt.Sprintf("the message type %q is not supported", messageType),
		"Check the feature list the peer returned in its hello answer, "+
			"and send only message types that list covers.",
		nil,
	)
}

// NewUnsupportedVersion builds the CodeUnsupportedVersion error for two
// peers whose protocol version ranges do not overlap. The ranges are
// inclusive and describe each side's lowest and highest version.
func NewUnsupportedVersion(clientMin, clientMax, serverMin, serverMax int) error {
	return New(
		CodeUnsupportedVersion,
		fmt.Sprintf(
			"no shared protocol version: the client speaks %d to %d and the holder speaks %d to %d",
			clientMin, clientMax, serverMin, serverMax,
		),
		"Use an Atomyx binary built from the same source as the process that holds the device.",
		nil,
	)
}
