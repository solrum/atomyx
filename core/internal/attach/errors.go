package attach

import "dev.atomyx/internal/diag"

var errResponseTooLarge = diag.New(
	diag.CodeUnknown,
	"the response is larger than the frame limit",
	"Ask again later, because the capture may be smaller, or read the file the holder writes instead.",
	ErrFrameTooLarge,
)

func invalidFrame(detail string) error {
	return diag.New(
		diag.CodeInvalidArgument,
		"invalid attach message: "+detail,
		"Send one JSON object per line in the shape the protocol defines.",
		nil,
	)
}
